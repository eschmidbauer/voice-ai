// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_livekit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
)

const (
	eosName = "livekitEndOfSpeech"

	optKeyThreshold      = "microphone.eos.threshold"
	optKeySilenceTimeout = "microphone.eos.silence_timeout"
	optKeyQuickTimeout   = "microphone.eos.quick_timeout"
	optKeyMaxHistory     = "microphone.eos.max_history_turns"

	defaultThreshold      = 0.0289
	defaultSilenceTimeout = 2000.0
	defaultQuickTimeout   = 200.0
	defaultMaxHistory     = 5.0
	defaultFallbackMs     = 500.0
)

// SpeechSegment represents accumulated speech with metadata.
type SpeechSegment struct {
	ContextID string
	Committed string // accumulated final transcripts
	Pending   string // latest interim transcript (not yet finalized)
	Timestamp time.Time
	Language  string
}

// FullText returns the complete transcript including any pending interim text.
func (s SpeechSegment) FullText() string {
	if s.Pending == "" {
		return s.Committed
	}
	if s.Committed == "" {
		return s.Pending
	}
	return s.Committed + " " + s.Pending
}

// command defines operations for the worker goroutine.
type command struct {
	ctx     context.Context
	timeout time.Duration
	segment SpeechSegment
	fireNow bool
	reset   bool
}

// eosState holds protected state for end-of-speech detection.
type eosState struct {
	segment       SpeechSegment
	callbackFired bool
	generation    uint64
}

// LivekitEOS detects end-of-speech using the LiveKit turn detector model
// with a hybrid approach: ONNX inference determines whether to use a quick
// or extended silence timeout, with fallback to standard silence on failure.
//
// Conversation history is built internally from packets flowing through
// Analyze — user turns are recorded when EOS fires, and assistant turns
// are recorded from LLMResponseDonePacket.
type LivekitEOS struct {
	logger   commons.Logger
	callback func(context.Context, ...internal_type.Packet) error

	// Model-based turn detection
	detector *TurnDetector

	// Conversation history built from packets (protected by mu)
	history []chatMessage

	// Configuration
	threshold      float64
	quickTimeout   time.Duration
	silenceTimeout time.Duration
	fallbackMs     time.Duration
	maxHistory     int

	// Worker orchestration
	cmdCh  chan command
	stopCh chan struct{}

	// State
	mu    sync.RWMutex
	state *eosState
}

// NewLivekitEndOfSpeech creates a new LiveKit model-based end-of-speech detector.
func NewLivekitEndOfSpeech(
	logger commons.Logger,
	onCallback func(context.Context, ...internal_type.Packet) error,
	opts utils.Option,
) (internal_type.EndOfSpeech, error) {
	start := time.Now()

	cfg := TurnDetectorConfig{}
	if v, err := opts.GetString("microphone.eos.livekit.model_path"); err == nil {
		cfg.ModelPath = v
	}
	if v, err := opts.GetString("microphone.eos.livekit.tokenizer_path"); err == nil {
		cfg.TokenizerPath = v
	}

	detector, err := NewTurnDetector(cfg)
	if err != nil {
		return nil, fmt.Errorf("livekit_eos: init turn detector: %w", err)
	}

	eos := &LivekitEOS{
		logger:         logger,
		callback:       onCallback,
		detector:       detector,
		threshold:      defaultThreshold,
		quickTimeout:   time.Duration(defaultQuickTimeout) * time.Millisecond,
		silenceTimeout: time.Duration(defaultSilenceTimeout) * time.Millisecond,
		fallbackMs:     time.Duration(defaultFallbackMs) * time.Millisecond,
		maxHistory:     int(defaultMaxHistory),
		cmdCh:          make(chan command, 32),
		stopCh:         make(chan struct{}),
		state:          &eosState{segment: SpeechSegment{}},
	}

	if v, err := opts.GetFloat64(optKeyThreshold); err == nil {
		eos.threshold = v
	}
	if v, err := opts.GetFloat64(optKeySilenceTimeout); err == nil {
		eos.silenceTimeout = time.Duration(v) * time.Millisecond
	}
	if v, err := opts.GetFloat64(optKeyQuickTimeout); err == nil {
		eos.quickTimeout = time.Duration(v) * time.Millisecond
	}
	if v, err := opts.GetFloat64(optKeyMaxHistory); err == nil {
		eos.maxHistory = int(v)
	}
	if v, err := opts.GetFloat64("microphone.eos.timeout"); err == nil {
		eos.fallbackMs = time.Duration(v) * time.Millisecond
	}

	go eos.worker()

	if onCallback != nil {
		_ = onCallback(context.Background(), internal_type.ConversationEventPacket{
			Name: "eos",
			Data: map[string]string{
				"type":     "initialized",
				"provider": eosName,
				"init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
			},
			Time: time.Now(),
		})
	}

	return eos, nil
}

// Name returns the component name.
func (eos *LivekitEOS) Name() string {
	return eosName
}

// Analyze processes incoming packets using the hybrid turn detection model.
// In addition to the standard EOS packet types (UserTextPacket, InterruptionPacket,
// SpeechToTextPacket), it also observes LLMResponseDonePacket to build
// conversation history for context-aware turn prediction.
func (eos *LivekitEOS) Analyze(ctx context.Context, pkt internal_type.Packet) error {
	switch p := pkt.(type) {
	case internal_type.UserTextPacket:
		if p.Text == "" {
			return nil
		}
		eos.mu.Lock()
		seg := SpeechSegment{ContextID: p.ContextId(), Committed: p.Text, Timestamp: time.Now()}
		eos.state.segment = seg
		eos.mu.Unlock()

		eos.callback(ctx,
			internal_type.InterimEndOfSpeechPacket{Speech: seg.Committed, ContextID: seg.ContextID},
			internal_type.ConversationEventPacket{Name: "eos", Data: map[string]string{"type": "interim", "speech": seg.Committed}},
		)
		eos.send(command{
			ctx:     ctx,
			segment: seg,
			fireNow: true,
		})

	case internal_type.InterruptionPacket:
		eos.mu.RLock()
		seg := eos.state.segment
		eos.mu.RUnlock()

		if seg.FullText() == "" {
			return nil
		}
		timeout := eos.computeTimeout(seg.FullText())
		eos.send(command{
			ctx:     ctx,
			segment: seg,
			timeout: timeout,
		})

	case internal_type.VadSpeechActivityPacket:
		eos.mu.RLock()
		seg := eos.state.segment
		eos.mu.RUnlock()

		if seg.FullText() == "" {
			return nil
		}
		timeout := eos.computeTimeout(seg.FullText())
		eos.send(command{
			ctx:     ctx,
			segment: seg,
			timeout: timeout,
		})

	case internal_type.SpeechToTextPacket:
		eos.mu.Lock()

		seg := eos.state.segment
		if seg.ContextID == "" {
			seg.ContextID = p.ContextId()
		}
		seg.Timestamp = time.Now()

		if p.Interim {
			seg.Pending = p.Script
		} else {
			if seg.Committed != "" {
				seg.Committed = fmt.Sprintf("%s %s", seg.Committed, p.Script)
			} else {
				seg.Committed = p.Script
			}
			seg.Pending = ""
		}

		if p.Language != "" {
			seg.Language = p.Language
		}

		eos.state.segment = seg
		fullText := seg.FullText()
		eos.mu.Unlock()

		if fullText == "" {
			return nil
		}

		eos.callback(ctx,
			internal_type.InterimEndOfSpeechPacket{Speech: fullText, ContextID: seg.ContextID},
			internal_type.ConversationEventPacket{Name: "eos", Data: map[string]string{"type": "interim", "speech": fullText}},
		)

		timeout := eos.computeTimeout(fullText)
		eos.send(command{
			ctx:     ctx,
			segment: seg,
			timeout: timeout,
		})

	case internal_type.LLMResponseDonePacket:
		if p.Text != "" {
			eos.mu.Lock()
			eos.history = append(eos.history, chatMessage{Role: "assistant", Content: p.Text})
			eos.mu.Unlock()
		}
	}

	return nil
}

// computeTimeout runs the turn detection model and returns the appropriate timeout.
// If P(im_end) > threshold, uses quickTimeout (user likely done).
// If P(im_end) < threshold, uses silenceTimeout (user likely still talking).
// On inference failure, falls back to the standard silence timeout.
func (eos *LivekitEOS) computeTimeout(currentText string) time.Duration {
	eos.mu.RLock()
	history := make([]chatMessage, len(eos.history))
	copy(history, eos.history)
	eos.mu.RUnlock()

	chatText := formatChatTemplateFromHistory(history, currentText, eos.maxHistory)
	if chatText == "" {
		return eos.fallbackMs
	}

	prob, err := eos.detector.Predict(chatText)
	if err != nil {
		if eos.logger != nil {
			eos.logger.Debugf("livekit_eos: inference failed: %v", err)
		}
		return eos.fallbackMs
	}

	if eos.logger != nil {
		eos.logger.Debugf("livekit_eos: P(im_end)=%.4f threshold=%.4f text=%q", prob, eos.threshold, currentText)
	}

	if prob > eos.threshold {
		return eos.quickTimeout
	}
	return eos.silenceTimeout
}

// send dispatches a command to the worker.
func (eos *LivekitEOS) send(cmd command) {
	select {
	case eos.cmdCh <- cmd:
	default:
		go func() { eos.cmdCh <- cmd }()
	}
}

// worker manages silence detection and callback invocation.
// Same pattern as the silence-based EOS: single goroutine, timer, generation counter.
func (eos *LivekitEOS) worker() {
	var (
		timer   *time.Timer
		timerC  <-chan time.Time
		gen     uint64
		ctx     context.Context
		segment SpeechSegment
	)

	cleanup := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
			timerC = nil
		}
	}

	for {
		select {
		case <-eos.stopCh:
			cleanup()
			return

		case cmd := <-eos.cmdCh:
			eos.mu.Lock()

			if cmd.reset {
				eos.state.callbackFired = false
				eos.state.generation++
				eos.state.segment = SpeechSegment{}
				eos.mu.Unlock()
				continue
			}

			if eos.state.callbackFired {
				eos.mu.Unlock()
				continue
			}

			if cmd.fireNow {
				eos.state.callbackFired = true
				seg := eos.state.segment
				cbCtx := cmd.ctx
				cleanup()
				eos.mu.Unlock()
				eos.fire(cbCtx, seg)
				continue
			}

			gen = eos.state.generation + 1
			eos.state.generation = gen
			ctx = cmd.ctx
			segment = cmd.segment
			cleanup()
			timer = time.NewTimer(cmd.timeout)
			timerC = timer.C
			eos.mu.Unlock()

		case <-timerC:
			eos.mu.Lock()
			if eos.state.callbackFired || gen != eos.state.generation {
				eos.mu.Unlock()
				continue
			}

			eos.state.callbackFired = true
			seg := segment
			cbCtx := ctx
			cleanup()
			eos.mu.Unlock()
			eos.fire(cbCtx, seg)
		}
	}
}

// fire triggers the callback, records the user turn in history, and enqueues reset.
// Emits EndOfSpeechPacket (triggers LLM) only when committed text exists.
// If only pending (interim) text, emits InterimEndOfSpeechPacket (display only).
func (eos *LivekitEOS) fire(ctx context.Context, seg SpeechSegment) {
	speech := seg.FullText()
	if speech == "" {
		return
	}

	// Record user turn in conversation history
	eos.mu.Lock()
	eos.history = append(eos.history, chatMessage{Role: "user", Content: speech})
	eos.mu.Unlock()

	if ctx.Err() != nil {
		ctx = context.Background()
	}

	wordCount := len(strings.Fields(speech))
	triggerAt := time.Now()
	event := internal_type.ConversationEventPacket{
		Name: "eos",
		Data: map[string]string{
			"type":               "detected",
			"provider":           eosName,
			"context_id":         seg.ContextID,
			"speech":             speech,
			"word_count":         fmt.Sprintf("%d", wordCount),
			"char_count":         fmt.Sprintf("%d", len(speech)),
			"text_to_trigger_ms": fmt.Sprintf("%d", triggerAt.Sub(seg.Timestamp).Milliseconds()),
		},
		Time: triggerAt,
	}

	if seg.Committed != "" {
		_ = eos.callback(ctx,
			internal_type.EndOfSpeechPacket{Speech: speech, ContextID: seg.ContextID, Language: seg.Language},
			event,
		)
	} else {
		_ = eos.callback(ctx,
			internal_type.InterimEndOfSpeechPacket{Speech: speech, ContextID: seg.ContextID},
			event,
		)
	}

	eos.send(command{reset: true})
}

// Close shuts down the detector and releases ONNX resources.
func (eos *LivekitEOS) Close() error {
	close(eos.stopCh)
	if eos.detector != nil {
		eos.detector.Destroy()
		eos.detector = nil
	}
	return nil
}
