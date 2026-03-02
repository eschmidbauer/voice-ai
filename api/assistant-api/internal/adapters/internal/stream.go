// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package adapter_internal

import (
	"context"
	"fmt"
	"time"

	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/types"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

// =============================================================================
// Talk - Main Entry Point
// =============================================================================

// Talk handles the main conversation loop for different streamer types.
// It processes incoming messages and manages the connection lifecycle.
//
// Shutdown relies on Recv() returning an error (EOF or context-cancelled)
// or a ConversationDisconnection message. All streamer implementations
// guarantee one of these when the connection ends.
func (t *genericRequestor) Talk(_ context.Context, auth types.SimplePrinciple) error {
	var initialized bool
	totalTime := time.Now()
	for {
		req, err := t.streamer.Recv()
		if err != nil {
			if initialized {
				// Persist completion metrics directly — the dispatcher goroutine
				// uses the streamer context, which is already cancelled when
				// Recv() returns an error. Routing through OnPacket/lowCh risks
				// the dispatcher exiting via ctx.Done() before it processes these
				// packets, leaving STATUS stuck at IN_PROGRESS and TIME_TAKEN
				// empty. Writing directly with a background context guarantees
				// the final state is persisted.
				completionMetrics := []*protos.Metric{
					{
						Name:        type_enums.STATUS.String(),
						Value:       "completed",
						Description: "Status of current conversation",
					},
					{
						Name:        type_enums.TIME_TAKEN.String(),
						Value:       fmt.Sprintf("%d", time.Since(totalTime)),
						Description: "Time taken to complete the conversation from the first message received to the end of the conversation.",
					},
				}
				if err := t.onAddMetrics(context.Background(), completionMetrics...); err != nil {
					t.logger.Errorf("talk: failed to persist completion metrics: %v", err)
				}
				t.metrics.Collect(context.Background(), observe.ConversationMetricRecord{
					ConversationID: fmt.Sprintf("%d", t.Conversation().Id),
					Metrics:        completionMetrics,
					Time:           time.Now(),
				})
				t.Disconnect(context.Background())
			}
			return nil
		}

		switch payload := req.(type) {
		case *protos.ConversationInitialization:
			t.logger.Infof("talk: received initialization, initialized=%v", initialized)
			if err := t.Connect(t.streamer.Context(), auth, payload); err != nil {
				t.logger.Errorf("unexpected error while connect assistant, might be problem in configuration %+v", err)
				return fmt.Errorf("talking.Connect error: %w", err)
			}
			initialized = true
			// Now that Connect() has finished (STT/TTS ready), trigger transport
			// setup for the requested mode. For AUDIO this starts the WebRTC
			// handshake; for TEXT or non-WebRTC streamers this is a no-op.
			t.streamer.NotifyMode(payload.GetStreamMode())

		case *protos.ConversationConfiguration:
			if initialized {
				switch payload.StreamMode {
				case protos.StreamMode_STREAM_MODE_TEXT:
					// Switching to text mode — tear down audio subsystems
					// only if they are currently active.
					if t.speechToTextTransformer != nil {
						utils.Go(t.streamer.Context(), func() {
							t.disconnectSpeechToText(t.streamer.Context())
						})
					}
					if t.textToSpeechTransformer != nil {
						utils.Go(t.streamer.Context(), func() {
							t.disconnectTextToSpeech(t.streamer.Context())
						})
					}
					t.SwitchMode(type_enums.TextMode)
				case protos.StreamMode_STREAM_MODE_AUDIO:
					// Switching to audio mode — initialize subsystems synchronously
					// so they are ready before audio packets arrive from WebRTC.
					if t.textToSpeechTransformer == nil {
						t.initializeTextToSpeech(t.streamer.Context())
					}
					if t.speechToTextTransformer == nil {
						if err := t.initializeSpeechToText(t.streamer.Context()); err != nil {
							t.logger.Errorf("failed to initialize speech-to-text on mode switch: %v", err)
						}
					}
					t.SwitchMode(type_enums.AudioMode)
				}
			}

		case *protos.ConversationUserMessage:
			if initialized {
				switch msg := payload.GetMessage().(type) {
				case *protos.ConversationUserMessage_Audio:
					if err := t.OnPacket(t.streamer.Context(), internal_type.UserAudioPacket{Audio: msg.Audio}); err != nil {
						t.logger.Errorf("error processing user audio: %v", err)
					}
				case *protos.ConversationUserMessage_Text:
					if err := t.OnPacket(t.streamer.Context(), internal_type.UserTextPacket{Text: msg.Text}); err != nil {
						t.logger.Errorf("error processing user text: %v", err)
					}
				default:
					t.logger.Errorf("illegal input from the user %+v", msg)
				}
			}

		case *protos.ConversationMetadata:
			if initialized {
				if err := t.OnPacket(t.streamer.Context(),
					internal_type.ConversationMetadataPacket{
						ContextID: payload.GetAssistantConversationId(),
						Metadata:  payload.GetMetadata(),
					}); err != nil {
					t.logger.Errorf("error while accepting metadata: %v", err)
				}
			}

		case *protos.ConversationMetric:
			if initialized {
				if err := t.OnPacket(t.streamer.Context(),
					internal_type.ConversationMetricPacket{
						ContextID: payload.GetAssistantConversationId(),
						Metrics:   payload.GetMetrics(),
					}); err != nil {
					t.logger.Errorf("error while accepting metrics: %v", err)
				}
			}

		case *protos.ConversationDisconnection:
			if initialized {
				t.OnPacket(context.Background(),
					internal_type.ConversationMetadataPacket{
						ContextID: t.Conversation().Id,
						Metadata: []*protos.Metadata{{
							Key:   "disconnect_reason",
							Value: payload.GetType().String(),
						}},
					},
				)
			}
		}
	}
}

// Notify sends notifications to websocket for various events.
func (t *genericRequestor) Notify(ctx context.Context, actionDatas ...internal_type.Stream) error {
	for _, actionData := range actionDatas {
		t.streamer.Send(actionData)
	}
	return nil
}
