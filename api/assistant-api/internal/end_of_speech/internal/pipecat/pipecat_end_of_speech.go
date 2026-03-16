// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_pipecat

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
)

const (
	eosName = "pipecatSmartTurnEndOfSpeech"

	optPctThreshold      = "microphone.eos.threshold"
	optPctSilenceTimeout = "microphone.eos.silence_timeout"
	optPctQuickTimeout   = "microphone.eos.quick_timeout"

	defaultPctThreshold      = 0.5
	defaultPctSilenceTimeout = 2000.0
	defaultPctQuickTimeout   = 200.0
	defaultPctFallbackMs     = 500.0

	// maxAudioSamples is the maximum number of float32 samples to buffer (8s at 16kHz).
	maxAudioSamples = whisperMaxSamples
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

type command struct {
	ctx     context.Context
	timeout time.Duration
	segment SpeechSegment
	fireNow bool
	reset   bool
}

type eosState struct {
	segment       SpeechSegment
	callbackFired bool
	generation    uint64
}

// PipecatEOS detects end-of-speech using the Pipecat Smart Turn audio model.
// It processes raw audio through Whisper mel spectrogram extraction and an
// ONNX classifier to determine turn completion probability, then uses a
// hybrid quick/extended timeout based on the prediction.
//
// Audio is accumulated from UserAudioPacket into a rolling 8-second buffer.
// Text packets (SpeechToTextPacket, InterruptionPacket) trigger inference
// on the buffered audio.
type PipecatEOS struct {
	logger   commons.Logger
	callback func(context.Context, ...internal_type.Packet) error

	detector *PipecatDetector

	// Configuration
	threshold      float64
	quickTimeout   time.Duration
	silenceTimeout time.Duration
	fallbackMs     time.Duration

	// Rolling audio buffer (protected by mu)
	audioBuf []float32

	// Worker orchestration
	cmdCh  chan command
	stopCh chan struct{}

	// State
	mu    sync.RWMutex
	state *eosState
}

// NewPipecatEndOfSpeech creates a new Pipecat Smart Turn audio-based
// end-of-speech detector.
func NewPipecatEndOfSpeech(
	logger commons.Logger,
	onCallback func(context.Context, ...internal_type.Packet) error,
	opts utils.Option,
) (internal_type.EndOfSpeech, error) {
	start := time.Now()

	cfg := PipecatDetectorConfig{}
	if v, err := opts.GetString("microphone.eos.pipecat.model_path"); err == nil {
		cfg.ModelPath = v
	}

	detector, err := NewPipecatDetector(cfg)
	if err != nil {
		return nil, fmt.Errorf("pipecat_eos: init detector: %w", err)
	}

	eos := &PipecatEOS{
		logger:         logger,
		callback:       onCallback,
		detector:       detector,
		threshold:      defaultPctThreshold,
		quickTimeout:   time.Duration(defaultPctQuickTimeout) * time.Millisecond,
		silenceTimeout: time.Duration(defaultPctSilenceTimeout) * time.Millisecond,
		fallbackMs:     time.Duration(defaultPctFallbackMs) * time.Millisecond,
		audioBuf:       make([]float32, 0, maxAudioSamples),
		cmdCh:          make(chan command, 32),
		stopCh:         make(chan struct{}),
		state:          &eosState{segment: SpeechSegment{}},
	}

	if v, err := opts.GetFloat64(optPctThreshold); err == nil {
		eos.threshold = v
	}
	if v, err := opts.GetFloat64(optPctSilenceTimeout); err == nil {
		eos.silenceTimeout = time.Duration(v) * time.Millisecond
	}
	if v, err := opts.GetFloat64(optPctQuickTimeout); err == nil {
		eos.quickTimeout = time.Duration(v) * time.Millisecond
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

func (eos *PipecatEOS) Name() string {
	return eosName
}

// Analyze processes incoming packets. In addition to the standard EOS packet
// types, it also handles UserAudioPacket to accumulate raw audio for
// audio-based turn detection.
func (eos *PipecatEOS) Analyze(ctx context.Context, pkt internal_type.Packet) error {
	switch p := pkt.(type) {
	case internal_type.UserAudioPacket:
		eos.appendAudio(p.Audio)

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
			internal_type.ConversationEventPacket{
				Name: "eos",
				Data: map[string]string{"type": "interim", "speech": seg.Committed},
			},
		)
		eos.send(command{ctx: ctx, segment: seg, fireNow: true})

	case internal_type.InterruptionPacket:
		eos.mu.RLock()
		seg := eos.state.segment
		eos.mu.RUnlock()
		if seg.FullText() == "" {
			return nil
		}
		timeout := eos.computeTimeout()
		eos.send(command{ctx: ctx, segment: seg, timeout: timeout})

	case internal_type.VadSpeechActivityPacket:
		eos.mu.RLock()
		seg := eos.state.segment
		eos.mu.RUnlock()
		if seg.FullText() == "" {
			return nil
		}
		timeout := eos.computeTimeout()
		eos.send(command{ctx: ctx, segment: seg, timeout: timeout})

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
			internal_type.ConversationEventPacket{
				Name: "eos",
				Data: map[string]string{"type": "interim", "speech": fullText},
			},
		)

		timeout := eos.computeTimeout()
		eos.send(command{ctx: ctx, segment: seg, timeout: timeout})
	}

	return nil
}

// appendAudio converts PCM16 LE bytes to float32 and appends to the rolling buffer.
func (eos *PipecatEOS) appendAudio(pcm16 []byte) {
	if len(pcm16) < 2 {
		return
	}

	nSamples := len(pcm16) / 2
	samples := make([]float32, nSamples)
	for i := 0; i < nSamples; i++ {
		s := int16(binary.LittleEndian.Uint16(pcm16[i*2:]))
		samples[i] = float32(s) / 32768.0
	}

	eos.mu.Lock()
	eos.audioBuf = append(eos.audioBuf, samples...)
	// Keep only last 8 seconds
	if len(eos.audioBuf) > maxAudioSamples {
		excess := len(eos.audioBuf) - maxAudioSamples
		eos.audioBuf = eos.audioBuf[excess:]
	}
	eos.mu.Unlock()
}

// computeTimeout runs the audio turn detection model and returns the
// appropriate timeout. If P(complete) > threshold, uses quickTimeout.
// On inference failure, falls back to the standard silence timeout.
func (eos *PipecatEOS) computeTimeout() time.Duration {
	eos.mu.RLock()
	audio := make([]float32, len(eos.audioBuf))
	copy(audio, eos.audioBuf)
	eos.mu.RUnlock()

	if len(audio) == 0 {
		return eos.fallbackMs
	}

	prob, err := eos.detector.Predict(audio)
	if err != nil {
		if eos.logger != nil {
			eos.logger.Debugf("pipecat_eos: inference failed: %v", err)
		}
		return eos.fallbackMs
	}

	if eos.logger != nil {
		eos.logger.Debugf("pipecat_eos: P(complete)=%.4f threshold=%.4f audio_samples=%d", prob, eos.threshold, len(audio))
	}

	if prob > eos.threshold {
		return eos.quickTimeout
	}
	return eos.silenceTimeout
}

func (eos *PipecatEOS) send(cmd command) {
	select {
	case eos.cmdCh <- cmd:
	default:
		go func() { eos.cmdCh <- cmd }()
	}
}

func (eos *PipecatEOS) worker() {
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
				eos.audioBuf = eos.audioBuf[:0]
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

func (eos *PipecatEOS) fire(ctx context.Context, seg SpeechSegment) {
	speech := seg.FullText()
	if speech == "" {
		return
	}

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

func (eos *PipecatEOS) Close() error {
	close(eos.stopCh)
	if eos.detector != nil {
		eos.detector.Destroy()
		eos.detector = nil
	}
	return nil
}
