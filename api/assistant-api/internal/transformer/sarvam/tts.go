// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_transformer_sarvam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	sarvam_internal "github.com/rapidaai/api/assistant-api/internal/transformer/sarvam/internal"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

type sarvamTextToSpeech struct {
	*sarvamOption
	// context management
	ctx       context.Context
	ctxCancel context.CancelFunc

	mu         sync.Mutex
	connection *websocket.Conn
	contextId  string

	// TTS latency tracking
	ttsStartedAt  time.Time
	ttsMetricSent bool

	logger   commons.Logger
	onPacket func(pkt ...internal_type.Packet) error
}

func NewSarvamTextToSpeech(ctx context.Context, logger commons.Logger, credential *protos.VaultCredential,
	onPacket func(pkt ...internal_type.Packet) error,
	opts utils.Option) (internal_type.TextToSpeechTransformer, error) {
	sarvamOpts, err := NewSarvamOption(logger, credential, opts)
	if err != nil {
		logger.Errorf("sarvam-tts: initializing sarvam failed %+v", err)
		return nil, err
	}
	ct, ctxCancel := context.WithCancel(ctx)
	return &sarvamTextToSpeech{
		ctx:          ct,
		ctxCancel:    ctxCancel,
		logger:       logger,
		sarvamOption: sarvamOpts,
		onPacket:     onPacket,
	}, nil
}

// Initialize implements internal_transformer.OutputAudioTransformer.
func (rt *sarvamTextToSpeech) Initialize() error {
	headers := map[string][]string{
		"Api-Subscription-Key": {rt.GetKey()},
	}
	conn, _, err := websocket.DefaultDialer.Dial(rt.textToSpeechUrl(), headers)
	if err != nil {
		rt.logger.Errorf("sarvam-tts: unable to connect to websocket err: %v", err)
		return err
	}

	if err := conn.WriteJSON(rt.configureTextToSpeech()); err != nil {
		rt.logger.Errorf("sarvam-tts: error sending configuration: %v", err)
		conn.Close()
		return err
	}

	rt.mu.Lock()
	rt.connection = conn
	rt.mu.Unlock()

	rt.logger.Debugf("sarvam-tts: connection established")
	go rt.textToSpeechCallback(conn, rt.ctx)
	rt.onPacket(internal_type.ConversationEventPacket{
		Name: "tts",
		Data: map[string]string{
			"type":     "initialized",
			"provider": rt.Name(),
		},
		Time: time.Now(),
	})
	return nil
}

// Name implements internal_transformer.OutputAudioTransformer.
func (*sarvamTextToSpeech) Name() string {
	return "sarvam-text-to-speech"
}

func (rt *sarvamTextToSpeech) textToSpeechCallback(conn *websocket.Conn, ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			rt.logger.Infof("sarvam-tts: context cancelled, stopping response listener")
			return
		default:
		}

		_, audioChunk, err := conn.ReadMessage()
		if err != nil {
			if err := rt.Initialize(); err != nil {
				rt.logger.Errorf("sarvam-tts: failed to re-initialize after 408 timeout: %v", err)
			}
			return
		}

		var response sarvam_internal.SarvamTextToSpeechResponse
		if err := json.Unmarshal(audioChunk, &response); err != nil {
			rt.logger.Errorf("sarvam-tts: error parsing response chunk: %v", err)
			continue
		}

		// Handle different message types based on AsyncAPI spec
		switch response.Type {
		case "audio":
			audioData, err := response.Audio()
			if err != nil {
				rt.logger.Errorf("sarvam-tts: invalid audio data format")
				continue
			}
			payload := audioData.Audio
			rawAudioData, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				rt.logger.Errorf("sarvam-tts: error decoding audio data: %v", err)
				continue
			}
			rt.mu.Lock()
			contextId := rt.contextId
			startedAt := rt.ttsStartedAt
			metricSent := rt.ttsMetricSent
			if !metricSent && !startedAt.IsZero() {
				rt.ttsMetricSent = true
			}
			rt.mu.Unlock()
			if !metricSent && !startedAt.IsZero() {
				rt.onPacket(internal_type.MessageMetricPacket{
					ContextID: contextId,
					Metrics: []*protos.Metric{{
						Name:  "tts_latency_ms",
						Value: fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()),
					}},
				})
			}
			rt.onPacket(internal_type.TextToSpeechAudioPacket{ContextID: contextId, AudioChunk: rawAudioData})
		case "event":
			eventData, err := response.AsEvent()
			if err != nil {
				rt.logger.Errorf("sarvam-tts: invalid event data format")
				continue
			}
			rt.logger.Infof("sarvam-tts: received event data: %v", eventData)
		case "error":
			errData, err := response.AsError()
			if err != nil {
				rt.logger.Errorf("sarvam-tts: invalid error data format")
				continue
			}
			if errData.Code != nil && *errData.Code == 408 {
				if err := rt.Initialize(); err != nil {
					rt.logger.Errorf("sarvam-tts: failed to re-initialize after 408 timeout: %v", err)
				}
			}
		}
	}
}

func (rt *sarvamTextToSpeech) Transform(ctx context.Context, in internal_type.LLMPacket) error {
	rt.mu.Lock()
	if in.ContextId() != rt.contextId {
		rt.contextId = in.ContextId()
		rt.ttsStartedAt = time.Time{}
		rt.ttsMetricSent = false
	}
	connection := rt.connection
	rt.mu.Unlock()

	if connection == nil {
		return fmt.Errorf("sarvam-tts: websocket connection is not initialized")
	}

	switch input := in.(type) {
	case internal_type.InterruptionPacket:
		rt.mu.Lock()
		rt.ttsStartedAt = time.Time{}
		rt.ttsMetricSent = false
		rt.mu.Unlock()
		return nil
	case internal_type.LLMResponseDeltaPacket:
		rt.mu.Lock()
		if rt.ttsStartedAt.IsZero() {
			rt.ttsStartedAt = time.Now()
		}
		rt.mu.Unlock()
		if err := connection.WriteJSON(map[string]interface{}{
			"type": "text",
			"data": map[string]interface{}{
				"text": input.Text,
			},
		}); err != nil {
			rt.logger.Errorf("sarvam-tts: error writing text message to websocket: %v", err)
			return err
		}
		rt.onPacket(internal_type.ConversationEventPacket{
			Name: "tts",
			Data: map[string]string{
				"type": "speaking",
				"text": input.Text,
			},
			Time: time.Now(),
		})
	case internal_type.LLMResponseDonePacket:
		if err := connection.WriteJSON(map[string]interface{}{
			"type": "flush",
		}); err != nil {
			rt.logger.Errorf("sarvam-tts: error sending flush signal to websocket: %v", err)
			return err
		}
		now := time.Now()
		rt.mu.Lock()
		contextId := rt.contextId
		rt.mu.Unlock()
		rt.onPacket(
			internal_type.TextToSpeechEndPacket{ContextID: contextId},
			internal_type.ConversationEventPacket{
				Name: "tts",
				Data: map[string]string{
					"type": "completed",
				},
				Time: now,
			},
		)
		return nil
	default:
		return fmt.Errorf("sarvam-tts: unsupported input type %T", in)
	}
	return nil

}

func (rt *sarvamTextToSpeech) Close(ctx context.Context) error {
	rt.ctxCancel()
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.connection != nil {
		rt.connection.Close()
		rt.connection = nil
	}

	return nil
}
