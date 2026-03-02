// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_transformer_rime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	rime_internal "github.com/rapidaai/api/assistant-api/internal/transformer/rime/internal"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

type rimeTTS struct {
	*rimeOption
	ctx       context.Context
	ctxCancel context.CancelFunc

	mu        sync.Mutex
	contextId string

	ttsStartedAt  time.Time
	ttsMetricSent bool

	logger     commons.Logger
	connection *websocket.Conn
	onPacket   func(pkt ...internal_type.Packet) error
}

func NewRimeTextToSpeech(ctx context.Context, logger commons.Logger, credential *protos.VaultCredential,
	onPacket func(pkt ...internal_type.Packet) error,
	opts utils.Option) (internal_type.TextToSpeechTransformer, error) {
	rimeOpts, err := NewRimeOption(logger, credential, opts)
	if err != nil {
		logger.Errorf("rime-tts: initializing rime failed %+v", err)
		return nil, err
	}
	ctx2, contextCancel := context.WithCancel(ctx)
	return &rimeTTS{
		ctx:        ctx2,
		ctxCancel:  contextCancel,
		onPacket:   onPacket,
		logger:     logger,
		rimeOption: rimeOpts,
	}, nil
}

func (ct *rimeTTS) Initialize() error {
	start := time.Now()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+ct.GetKey())
	conn, resp, err := websocket.DefaultDialer.Dial(ct.GetTextToSpeechConnectionString(), header)
	if err != nil {
		ct.logger.Errorf("rime-tts: error while connecting to rime %s with response %v", err, resp)
		return err
	}

	ct.mu.Lock()
	ct.connection = conn
	defer ct.mu.Unlock()

	go ct.textToSpeechCallback(conn, ct.ctx)
	ct.onPacket(internal_type.ConversationEventPacket{
		Name: "tts",
		Data: map[string]string{
			"type":     "initialized",
			"provider": ct.Name(),
			"init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
		},
		Time: time.Now(),
	})
	return nil
}

func (*rimeTTS) Name() string {
	return "rime-text-to-speech"
}

func (rt *rimeTTS) textToSpeechCallback(conn *websocket.Conn, ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			rt.logger.Infof("rime-tts: context cancelled, stopping response listener")
			return
		default:
			_, audioChunk, err := conn.ReadMessage()
			if err != nil {
				if errors.Is(err, io.EOF) || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					rt.logger.Infof("rime-tts: websocket closed gracefully")
					return
				}
				rt.logger.Errorf("rime-tts: websocket read error: %v", err)
				return
			}
			var audioData rime_internal.RimeTextToSpeechResponse
			if err := json.Unmarshal(audioChunk, &audioData); err != nil {
				rt.logger.Errorf("rime-tts: error parsing audio chunk: %v", err)
				continue
			}

			switch audioData.Type {
			case "chunk":
				if rawAudioData, err := base64.StdEncoding.DecodeString(audioData.Data); err == nil {
					rt.mu.Lock()
					startedAt := rt.ttsStartedAt
					metricSent := rt.ttsMetricSent
					ctxId := rt.contextId
					if !metricSent && !startedAt.IsZero() {
						rt.ttsMetricSent = true
					}
					rt.mu.Unlock()
					if ctxId != "" {
						if !metricSent && !startedAt.IsZero() {
							rt.onPacket(internal_type.MessageMetricPacket{
								ContextID: ctxId,
								Metrics: []*protos.Metric{{
									Name:  "tts_latency_ms",
									Value: fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()),
								}},
							})
						}
						rt.onPacket(internal_type.TextToSpeechAudioPacket{ContextID: ctxId, AudioChunk: rawAudioData})
					}
				} else {
					rt.logger.Errorf("rime-tts: error decoding base64 audio: %v", err)
				}
			case "error":
				rt.logger.Errorf("rime-tts: server error: %s", audioData.Message)
			}
		}
	}
}

func (t *rimeTTS) Transform(ctx context.Context, in internal_type.LLMPacket) error {
	t.mu.Lock()
	cnn := t.connection
	currentCtx := t.contextId
	if in.ContextId() != t.contextId {
		t.contextId = in.ContextId()
		t.ttsStartedAt = time.Time{}
		t.ttsMetricSent = false
	}
	t.mu.Unlock()

	if cnn == nil {
		return fmt.Errorf("rime-tts: websocket connection is not initialized")
	}

	switch input := in.(type) {
	case internal_type.InterruptionPacket:
		if currentCtx != "" {
			t.mu.Lock()
			t.ttsStartedAt = time.Time{}
			t.ttsMetricSent = false
			t.mu.Unlock()
			if err := cnn.WriteJSON(map[string]interface{}{
				"operation": "clear",
			}); err != nil {
				t.logger.Errorf("rime-tts: unable to send clear operation: %v", err)
			}
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
		if err := cnn.WriteJSON(map[string]interface{}{
			"text":      input.Text,
			"contextId": t.contextId,
		}); err != nil {
			t.logger.Errorf("rime-tts: unable to write json for text to speech: %v", err)
		}
		t.onPacket(internal_type.ConversationEventPacket{
			Name: "tts",
			Data: map[string]string{
				"type": "speaking",
				"text": input.Text,
			},
			Time: time.Now(),
		})
	case internal_type.LLMResponseDonePacket:
		if err := cnn.WriteJSON(map[string]interface{}{
			"operation": "eos",
		}); err != nil {
			t.logger.Errorf("rime-tts: unable to send eos operation: %v", err)
		}
		t.mu.Lock()
		ctxId := t.contextId
		t.mu.Unlock()
		if ctxId != "" {
			t.onPacket(
				internal_type.TextToSpeechEndPacket{ContextID: ctxId},
				internal_type.ConversationEventPacket{
					Name: "tts",
					Data: map[string]string{"type": "completed"},
					Time: time.Now(),
				},
			)
		}
		return nil
	default:
		return fmt.Errorf("rime-tts: unsupported input type %T", in)
	}
	return nil
}

func (t *rimeTTS) Close(ctx context.Context) error {
	t.ctxCancel()
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connection != nil {
		t.connection.Close()
		t.connection = nil
	}
	return nil
}
