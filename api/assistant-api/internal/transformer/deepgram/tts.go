// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_transformer_deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	deepgram_internal "github.com/rapidaai/api/assistant-api/internal/transformer/deepgram/internal"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	utils "github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

/*
Deepgram Continuous Streaming TTS
Reference: https://developers.deepgram.com/reference/text-to-speech/speak-streaming
*/

type deepgramTTS struct {
	*deepgramOption
	// context management
	ctx       context.Context
	ctxCancel context.CancelFunc
	contextId string
	mu        sync.Mutex

	// TTS latency tracking
	ttsStartedAt  time.Time
	ttsMetricSent bool

	logger     commons.Logger
	connection *websocket.Conn
	onPacket   func(pkt ...internal_type.Packet) error
	normalizer internal_type.TextNormalizer
}

func NewDeepgramTextToSpeech(ctx context.Context, logger commons.Logger, credential *protos.VaultCredential,
	onPacket func(pkt ...internal_type.Packet) error,
	opts utils.Option) (internal_type.TextToSpeechTransformer, error) {

	dGoptions, err := NewDeepgramOption(logger, credential, opts)
	if err != nil {
		logger.Errorf("deepgram-tts: error while intializing deepgram text to speech")
		return nil, err
	}
	ctx2, cancel := context.WithCancel(ctx)
	return &deepgramTTS{
		deepgramOption: dGoptions,
		ctx:            ctx2,
		ctxCancel:      cancel,
		logger:         logger,
		onPacket:       onPacket,
		normalizer:     NewDeepgramNormalizer(logger, opts),
	}, nil
}

// Initialize implements internal_transformer.OutputAudioTransformer.
func (t *deepgramTTS) Initialize() error {

	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("token %s", t.GetKey()))
	conn, resp, err := websocket.DefaultDialer.Dial(t.GetTextToSpeechConnectionString(), header)
	if err != nil {
		t.logger.Errorf("deepgram-tts: websocket dial failed err=%v resp=%v", err, resp)
		return err
	}

	t.mu.Lock()
	t.connection = conn
	t.mu.Unlock()

	go t.textToSpeechCallback(conn, t.ctx)
	t.onPacket(internal_type.ConversationEventPacket{
		Name: "tts",
		Data: map[string]string{
			"type":     "initialized",
			"provider": t.Name(),
		},
		Time: time.Now(),
	})
	return nil
}

// Name implements internal_transformer.OutputAudioTransformer.
func (*deepgramTTS) Name() string {
	return "deepgram-text-to-speech"
}

// readLoop handles server → client messages
func (t *deepgramTTS) textToSpeechCallback(conn *websocket.Conn, ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			t.logger.Infof("deepgram-tts: context cancelled, stopping read loop")
			return
		default:
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				if errors.Is(err, io.EOF) || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					t.logger.Infof("deepgram-tts: websocket closed gracefully")
					return
				}
				t.logger.Errorf("deepgram-tts: read error %v", err)
				return
			}

			if msgType == websocket.BinaryMessage {
				t.mu.Lock()
				startedAt := t.ttsStartedAt
				metricSent := t.ttsMetricSent
				ctxId := t.contextId
				if !metricSent && !startedAt.IsZero() {
					t.ttsMetricSent = true
				}
				t.mu.Unlock()
				if !metricSent && !startedAt.IsZero() {
					t.onPacket(internal_type.MessageMetricPacket{
						ContextID: ctxId,
						Metrics: []*protos.Metric{{
							Name:  "tts_latency_ms",
							Value: fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()),
						}},
					})
				}
				t.onPacket(internal_type.TextToSpeechAudioPacket{
					ContextID:  ctxId,
					AudioChunk: data,
				})
				continue
			}

			var envelope *deepgram_internal.DeepgramTextToSpeechResponse
			if err := json.Unmarshal(data, &envelope); err != nil {
				continue
			}

			switch envelope.Type {
			case "Metadata":
				continue

			case "Flushed":
				now := time.Now()
				t.onPacket(
					internal_type.TextToSpeechEndPacket{ContextID: t.contextId},
					internal_type.ConversationEventPacket{
						Name:      "tts",
						Data: map[string]string{
							"type": "completed",
						},
						Time: now,
					},
				)
				continue

			case "Cleared":
				// ignoreing metadata for now
				continue

			case "Warning":
				t.logger.Warnf("deepgram-tts warning code=%s message=%s", envelope.Code, envelope.Message)
			}
		}
	}
}

// Transform streams text into Deepgram
func (t *deepgramTTS) Transform(ctx context.Context, in internal_type.LLMPacket) error {
	t.mu.Lock()
	conn := t.connection
	currentCtx := t.contextId
	if in.ContextId() != t.contextId {
		t.contextId = in.ContextId()
		t.ttsStartedAt = time.Time{}
		t.ttsMetricSent = false
	}
	t.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("deepgram-tts: websocket not initialized")
	}

	switch input := in.(type) {
	case internal_type.InterruptionPacket:
		if currentCtx != "" {
			_ = conn.WriteJSON(map[string]interface{}{
				"type": "Clear",
			})
			t.mu.Lock()
			t.ttsStartedAt = time.Time{}
			t.ttsMetricSent = false
			t.mu.Unlock()
			t.onPacket(internal_type.ConversationEventPacket{
				Name: "tts",
				Data: map[string]string{"type": "interrupted"},
				Time: time.Now(),
			})
		}
		return nil
	case internal_type.LLMResponseDeltaPacket:
		t.mu.Lock()
		if t.ttsStartedAt.IsZero() {
			t.ttsStartedAt = time.Now()
		}
		t.mu.Unlock()
		normalized := t.normalizer.Normalize(ctx, input.Text)
		if err := conn.WriteJSON(map[string]interface{}{
			"type": "Speak",
			"text": normalized,
		}); err != nil {
			t.logger.Errorf("deepgram-tts: failed to send Speak message %v", err)
		}
		t.onPacket(internal_type.ConversationEventPacket{
			Name: "tts",
			Data: map[string]string{
				"type": "speaking",
				"text": normalized,
			},
			Time: time.Now(),
		})
		return nil
	case internal_type.LLMResponseDonePacket:
		if err := conn.WriteJSON(map[string]string{"type": "Flush"}); err != nil {
			t.logger.Errorf("deepgram-tts: failed to send Flush %v", err)
			return err
		}
		return nil
	default:
		return fmt.Errorf("deepgram-tts: unsupported input type %T", in)
	}

}

// Close gracefully closes the Deepgram connection
func (t *deepgramTTS) Close(ctx context.Context) error {
	t.ctxCancel()
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connection != nil {
		_ = t.connection.WriteJSON(map[string]string{
			"type": "Close",
		})
		t.connection.Close()
		t.connection = nil
	}
	return nil
}
