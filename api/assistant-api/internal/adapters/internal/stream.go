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

	obs "github.com/rapidaai/api/assistant-api/internal/observe"
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
				t.emitCallCompletion(totalTime)
				t.Disconnect(context.Background())
			}
			return nil
		}

		switch payload := req.(type) {
		case *protos.ConversationInitialization:
			if err := t.Connect(t.streamer.Context(), auth, payload); err != nil {
				t.OnPacket(context.Background(), internal_type.ConversationEventPacket{
					ContextID: t.GetID(),
					Name:      obs.ComponentSession,
					Data:      map[string]string{obs.DataType: obs.EventConnectFailed, obs.DataError: err.Error()},
					Time:      time.Now(),
				})
				t.onAddMetrics(context.Background(), &protos.Metric{
					Name:        type_enums.CONVERSATION_STATUS.String(),
					Value:       "FAILED",
					Description: fmt.Sprintf("Connection failed: %v", err),
				})
				return fmt.Errorf("talking.Connect error: %w", err)
			}
			initialized = true
			t.OnPacket(t.streamer.Context(), internal_type.ConversationEventPacket{
				ContextID: t.GetID(),
				Name:      obs.ComponentSession,
				Data:      map[string]string{obs.DataType: obs.EventConnected, obs.DataMode: payload.GetStreamMode().String()},
				Time:      time.Now(),
			})
			t.streamer.NotifyMode(payload.GetStreamMode())

		case *protos.ConversationConfiguration:
			if initialized {
				prevMode := t.GetMode().String()
				switch payload.StreamMode {
				case protos.StreamMode_STREAM_MODE_TEXT:
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
				t.OnPacket(t.streamer.Context(), internal_type.ConversationEventPacket{
					ContextID: t.GetID(),
					Name:      obs.ComponentSession,
					Data:      map[string]string{obs.DataType: obs.EventModeSwitch, obs.DataFrom: prevMode, obs.DataTo: payload.StreamMode.String()},
					Time:      time.Now(),
				})
			}

		case *protos.ConversationUserMessage:
			if initialized {
				switch msg := payload.GetMessage().(type) {
				case *protos.ConversationUserMessage_Audio:
					if err := t.OnPacket(t.streamer.Context(), internal_type.UserAudioReceivedPacket{ContextID: t.GetID(), Audio: msg.Audio}); err != nil {
						t.logger.Errorf("error processing user audio: %v", err)
					}
				case *protos.ConversationUserMessage_Text:
					if err := t.OnPacket(t.streamer.Context(), internal_type.UserTextReceivedPacket{ContextID: t.GetID(), Text: msg.Text}); err != nil {
						t.logger.Errorf("error processing user text: %v", err)
					}
				default:
					t.logger.Errorf("illegal input from the user %+v", msg)
				}
			}

		case *protos.ConversationToolCallResult:
			if initialized {
				t.OnPacket(t.streamer.Context(), internal_type.LLMToolResultPacket{
					ToolID:    payload.GetToolId(),
					Name:      payload.GetName(),
					ContextID: payload.GetId(),
					Action:    payload.GetAction(),
					Result:    payload.GetResult(),
				})
			}

		case *protos.ConversationBridgeUserAudio:
			if initialized {
				t.OnPacket(t.streamer.Context(), internal_type.RecordUserAudioPacket{ContextID: t.GetID(), Audio: payload.Audio})
			}

		case *protos.ConversationBridgeOperatorAudio:
			if initialized {
				t.OnPacket(t.streamer.Context(), internal_type.RecordAssistantAudioPacket{ContextID: t.GetID(), Audio: payload.Audio})
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

		case *protos.ConversationEvent:
			if initialized {
				if err := t.OnPacket(t.streamer.Context(), internal_type.ConversationEventPacket{
					Name: payload.Name,
					Data: payload.Data,
					Time: payload.Time.AsTime(),
				}); err != nil {
					t.logger.Errorf("error processing channel event: %v", err)
				}
			}

		case *protos.ConversationDisconnection:
			if initialized {
				t.OnPacket(context.Background(), internal_type.ConversationEventPacket{
					ContextID: t.GetID(),
					Name:      obs.ComponentSession,
					Data:      map[string]string{obs.DataType: obs.EventDisconnectRequested, obs.DataReason: payload.GetType().String()},
					Time:      time.Now(),
				})
				t.OnPacket(context.Background(),
					internal_type.ConversationMetadataPacket{
						ContextID: t.Conversation().Id,
						Metadata: []*protos.Metadata{{
							Key:   "disconnect_reason",
							Value: payload.GetType().String(),
						}},
					},
				)
				t.emitCallCompletion(totalTime)
				t.Disconnect(context.Background())
				return nil
			}
		}
	}
}

// emitCallCompletion persists final metrics and events when the talk loop exits.
// Written directly with a background context because the dispatcher goroutine's
// context is already cancelled when Recv() returns an error.
func (t *genericRequestor) emitCallCompletion(startTime time.Time) {
	duration := time.Since(startTime)
	completionMetrics := []*protos.Metric{
		{
			Name:        type_enums.CONVERSATION_STATUS.String(),
			Value:       type_enums.CONVERSATION_COMPLETE.String(),
			Description: "Status of current conversation",
		},
		{
			Name:        type_enums.CONVERSATION_DURATION.String(),
			Value:       fmt.Sprintf("%d", duration),
			Description: "Conversation duration from first message to end",
		},
	}
	if err := t.onAddMetrics(context.Background(), completionMetrics...); err != nil {
		t.logger.Errorf("talk: failed to persist completion metrics: %v", err)
	}
	if t.observer != nil {
		t.observer.MetricCollectors().Collect(context.Background(), obs.ConversationMetricRecord{
			ConversationID: fmt.Sprintf("%d", t.Conversation().Id),
			Metrics:        completionMetrics,
			Time:           time.Now(),
		})
		t.observer.EventCollectors().Collect(context.Background(), obs.EventRecord{
			MessageID: t.GetID(),
			Name:      obs.ComponentSession,
			Data: map[string]string{
				obs.DataType:     obs.EventCompleted,
				obs.DataDuration: fmt.Sprintf("%d", duration.Milliseconds()),
				obs.DataMessages: fmt.Sprintf("%d", len(t.GetHistories())),
			},
			Time: time.Now(),
		})
	}
}

// Notify sends notifications to websocket for various events.
func (t *genericRequestor) Notify(ctx context.Context, actionDatas ...internal_type.Stream) error {
	for _, actionData := range actionDatas {
		t.streamer.Send(actionData)
	}
	return nil
}
