// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_transformer_cartesia

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	cartesia_internal "github.com/rapidaai/api/assistant-api/internal/transformer/cartesia/internal"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

type cartesiaTTS struct {
	*cartesiaOption
	mu sync.Mutex
	// context management
	ctx       context.Context
	ctxCancel context.CancelFunc

	contextId string

	// TTS latency tracking
	ttsStartedAt  time.Time
	ttsMetricSent bool

	logger     commons.Logger
	connection *websocket.Conn
	onPacket   func(pkt ...internal_type.Packet) error
}

func NewCartesiaTextToSpeech(ctx context.Context, logger commons.Logger, credential *protos.VaultCredential,
	onPacket func(pkt ...internal_type.Packet) error,
	opts utils.Option) (internal_type.TextToSpeechTransformer, error) {
	cartesiaOpts, err := NewCartesiaOption(logger, credential, opts)
	if err != nil {
		logger.Errorf("intializing cartesia failed %+v", err)
		return nil, err
	}

	ct, ctxCancel := context.WithCancel(ctx)
	return &cartesiaTTS{
		cartesiaOption: cartesiaOpts,
		logger:         logger,
		ctx:            ct,
		ctxCancel:      ctxCancel,
		onPacket:       onPacket,
	}, nil
}

func (ct *cartesiaTTS) Initialize() error {
	conn, _, err := websocket.DefaultDialer.Dial(ct.GetTextToSpeechConnectionString(), nil)
	if err != nil {
		ct.logger.Errorf("cartesia-stt: unable to dial %v", err)
		return err
	}
	ct.mu.Lock()
	ct.connection = conn
	ct.mu.Unlock()

	go ct.textToSpeechCallback(ct.connection, ct.ctx)
	ct.onPacket(internal_type.ConversationEventPacket{
		Name: "tts",
		Data: map[string]string{
			"type":     "initialized",
			"provider": ct.Name(),
		},
		Time: time.Now(),
	})
	return nil
}

func (cst *cartesiaTTS) textToSpeechCallback(conn *websocket.Conn, ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			cst.logger.Infof("cartesia-tts: context cancelled, stopping response listener")
			return
		default:
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var payload cartesia_internal.TextToSpeechOuput
			if err := json.Unmarshal(msg, &payload); err != nil {
				cst.logger.Errorf("cartesia-tts: invalid json from cartesia error : %v", err)
				continue
			}
			if payload.Done {
				_ = cst.onPacket(
					internal_type.TextToSpeechEndPacket{ContextID: payload.ContextID},
					internal_type.ConversationEventPacket{
						Name: "tts",
						Data: map[string]string{"type": "completed"},
						Time: time.Now(),
					},
				)
				continue
			}
			if payload.Data == "" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(payload.Data)
			if err != nil {
				cst.logger.Error("cartesia-tts: failed to decode audio payload error: %v", err)
				continue
			}
			cst.mu.Lock()
			startedAt := cst.ttsStartedAt
			metricSent := cst.ttsMetricSent
			ctxId := payload.ContextID
			if !metricSent && !startedAt.IsZero() {
				cst.ttsMetricSent = true
			}
			cst.mu.Unlock()
			if !metricSent && !startedAt.IsZero() {
				_ = cst.onPacket(internal_type.MessageMetricPacket{
					ContextID: ctxId,
					Metrics: []*protos.Metric{{
						Name:  "tts_latency_ms",
						Value: fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()),
					}},
				})
			}
			_ = cst.onPacket(internal_type.TextToSpeechAudioPacket{ContextID: ctxId, AudioChunk: decoded})
		}
	}
}

// Name returns the name of this transformer.
func (*cartesiaTTS) Name() string {
	return "cartesia-text-to-speech"
}

func (ct *cartesiaTTS) Transform(ctx context.Context, in internal_type.LLMPacket) error {
	ct.mu.Lock()
	conn := ct.connection
	currentCtx := ct.contextId
	if in.ContextId() != ct.contextId {
		ct.contextId = in.ContextId()
		ct.ttsStartedAt = time.Time{}
		ct.ttsMetricSent = false
	}
	ct.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("cartesia-tts: websocket connection is not initialized")
	}

	switch input := in.(type) {
	case internal_type.InterruptionPacket:
		if currentCtx != "" {
			_ = conn.WriteJSON(map[string]interface{}{
				"context_id": currentCtx,
				"cancel":     true,
			})
			ct.mu.Lock()
			ct.ttsStartedAt = time.Time{}
			ct.ttsMetricSent = false
			ct.mu.Unlock()
			ct.onPacket(internal_type.ConversationEventPacket{
				Name: "tts",
				Data: map[string]string{"type": "interrupted"},
				Time: time.Now(),
			})
		}
		return nil
	case internal_type.LLMResponseDeltaPacket:
		ct.mu.Lock()
		if ct.ttsStartedAt.IsZero() {
			ct.ttsStartedAt = time.Now()
		}
		ct.mu.Unlock()
		message := ct.GetTextToSpeechInput(input.Text, map[string]interface{}{"continue": true, "context_id": ct.contextId, "max_buffer_delay_ms": "0ms"})
		if err := conn.WriteJSON(message); err != nil {
			return err
		}
		ct.onPacket(internal_type.ConversationEventPacket{
			Name: "tts",
			Data: map[string]string{
				"type": "speaking",
				"text": input.Text,
			},
			Time: time.Now(),
		})
	case internal_type.LLMResponseDonePacket:
		message := ct.GetTextToSpeechInput("", map[string]interface{}{"continue": false, "flush": true, "context_id": ct.contextId})
		if err := conn.WriteJSON(message); err != nil {
			return err
		}
	default:
		return fmt.Errorf("cartesia-tts: unsupported input type %T", in)
	}
	return nil

}

func (ct *cartesiaTTS) Close(ctx context.Context) error {
	ct.ctxCancel()

	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.connection != nil {
		_ = ct.connection.Close()
	}
	return nil
}
