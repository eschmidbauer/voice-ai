// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_twilio_telephony

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_telephony_base "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/base"
	internal_twilio "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/twilio/internal"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	rapida16kConfig = internal_audio.NewLinear16khzMonoAudioConfig()
	mulaw8kConfig   = internal_audio.NewMulaw8khzMonoAudioConfig()
)

type twilioWebsocketStreamer struct {
	internal_telephony_base.BaseTelephonyStreamer

	streamID   string
	connection *websocket.Conn
	writeMu    sync.Mutex
	closed     atomic.Bool
}

func NewTwilioWebsocketStreamer(logger commons.Logger, connection *websocket.Conn, cc *callcontext.CallContext, vaultCred *protos.VaultCredential) internal_type.Streamer {
	tws := &twilioWebsocketStreamer{
		BaseTelephonyStreamer: internal_telephony_base.NewBaseTelephonyStreamer(
			logger, cc, vaultCred,
			internal_telephony_base.WithSourceAudioConfig(internal_audio.NewMulaw8khzMonoAudioConfig()),
		),
		streamID:   "",
		connection: connection,
	}
	go tws.runWebSocketReader()
	return tws
}

func (tws *twilioWebsocketStreamer) runWebSocketReader() {
	conn := tws.connection
	if conn == nil {
		return
	}
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if msg := tws.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				tws.Input(msg)
			}
			tws.BaseStreamer.Cancel()
			return
		}
		var mediaEvent internal_twilio.TwilioMediaEvent
		if err := json.Unmarshal(message, &mediaEvent); err != nil {
			tws.Logger.Error("Failed to unmarshal Twilio media event", "error", err.Error())
			continue
		}
		switch mediaEvent.Event {
		case "connected":
			tws.Input(&protos.ConversationEvent{
				Name: "channel",
				Data: map[string]string{"type": "connected", "provider": "twilio"},
				Time: timestamppb.Now(),
			})
		case "start":
			tws.handleStartEvent(mediaEvent)
			tws.Input(tws.CreateConnectionRequest())
			tws.Input(&protos.ConversationEvent{
				Name: "channel",
				Data: map[string]string{"type": "stream_started", "provider": "twilio", "stream_id": tws.streamID},
				Time: timestamppb.Now(),
			})
		case "media":
			msg, _ := tws.handleMediaEvent(mediaEvent)
			if msg != nil {
				tws.Input(msg)
			}
		case "stop":
			tws.Logger.Info("Twilio stream stopped")
			if msg := tws.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				tws.Input(msg)
			}
			tws.Cancel()
			return
		default:
			tws.Logger.Warn("Unhandled Twilio event", "event", mediaEvent.Event)
		}
	}
}

func (tws *twilioWebsocketStreamer) Send(response internal_type.Stream) error {
	if tws.connection == nil {
		return nil
	}
	switch data := response.(type) {
	case *protos.ConversationAssistantMessage:
		switch content := data.Message.(type) {
		case *protos.ConversationAssistantMessage_Audio:
			audioData, err := tws.Resampler().Resample(content.Audio, rapida16kConfig, mulaw8kConfig)
			if err != nil {
				tws.Logger.Warnw("Failed to resample output audio to mulaw 8kHz, forwarding raw bytes",
					"error", err.Error(),
				)
				audioData = content.Audio
			}

			var sendErr error
			tws.WithOutputBuffer(func(buf *bytes.Buffer) {
				buf.Write(audioData)
				for buf.Len() >= tws.OutputFrameSize() && tws.streamID != "" {
					chunk := buf.Next(tws.OutputFrameSize())
					if err := tws.sendTwilioMessage("media", map[string]interface{}{
						"payload": tws.Encoder().EncodeToString(chunk),
					}); err != nil {
						tws.Logger.Error("Failed to send audio chunk", "error", err.Error())
						sendErr = err
						return
					}
				}
				if data.GetCompleted() && buf.Len() > 0 {
					remainingChunk := buf.Bytes()
					if err := tws.sendTwilioMessage("media", map[string]interface{}{
						"payload": tws.Encoder().EncodeToString(remainingChunk),
					}); err != nil {
						tws.Logger.Errorf("Failed to send final audio chunk", "error", err.Error())
						sendErr = err
						return
					}
					buf.Reset()
				}
			})
			return sendErr
		}
	case *protos.ConversationInterruption:
		if data.Type == protos.ConversationInterruption_INTERRUPTION_TYPE_WORD {
			tws.ResetOutputBuffer()
			if err := tws.sendTwilioMessage("clear", nil); err != nil {
				tws.Logger.Errorf("Error sending clear command:", err)
			}
		}
	case *protos.ConversationToolCall:
		switch data.GetAction() {
		case protos.ToolCallAction_TOOL_CALL_ACTION_END_CONVERSATION:
			if tws.GetConversationUuid() != "" {
				client, err := twilioClient(tws.VaultCredential())
				if err != nil {
					tws.Logger.Errorf("Error creating Twilio client:", err)
					tws.Cancel()
					return nil
				}
				params := &openapi.UpdateCallParams{}
				params.SetStatus("completed")
				if _, err := client.Api.UpdateCall(tws.GetConversationUuid(), params); err != nil {
					tws.Logger.Errorf("Error ending Twilio call:", err)
					tws.Cancel()
					return nil
				}
			}
			tws.Cancel()
		case protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION:
			to := data.GetArgs()["to"]
			if to == "" || tws.GetConversationUuid() == "" {
				tws.Input(&protos.ConversationToolCallResult{
					Id:     data.GetId(),
					ToolId: data.GetToolId(), Name: data.GetName(), Action: data.GetAction(),
					Result: map[string]string{"status": "failed", "reason": "missing target or call ID"},
				})
				return nil
			}
			tws.Logger.Infow("Transferring Twilio call", "to", to)
			client, err := twilioClient(tws.VaultCredential())
			if err != nil {
				tws.Input(&protos.ConversationToolCallResult{
					Id:     data.GetId(),
					ToolId: data.GetToolId(), Name: data.GetName(), Action: data.GetAction(),
					Result: map[string]string{"status": "failed", "reason": fmt.Sprintf("twilio client error: %v", err)},
				})
				return nil
			}
			params := &openapi.UpdateCallParams{}
			params.SetTwiml(fmt.Sprintf(`<Response><Dial>%s</Dial></Response>`, to))
			if _, err := client.Api.UpdateCall(tws.GetConversationUuid(), params); err != nil {
				tws.Input(&protos.ConversationToolCallResult{
					Id:     data.GetId(),
					ToolId: data.GetToolId(), Name: data.GetName(), Action: data.GetAction(),
					Result: map[string]string{"status": "failed", "reason": fmt.Sprintf("transfer failed: %v", err)},
				})
			} else {
				tws.Input(&protos.ConversationToolCallResult{
					Id:     data.GetId(),
					ToolId: data.GetToolId(), Name: data.GetName(), Action: data.GetAction(),
					Result: map[string]string{"status": "completed"},
				})
			}
			tws.Cancel()
		}
	}
	return nil
}

func (tws *twilioWebsocketStreamer) handleStartEvent(mediaEvent internal_twilio.TwilioMediaEvent) {
	tws.streamID = mediaEvent.StreamSid
}

func (tws *twilioWebsocketStreamer) GetConversationUuid() string {
	return tws.ChannelUUID
}

func (tws *twilioWebsocketStreamer) Cancel() error {
	if !tws.closed.CompareAndSwap(false, true) {
		return nil
	}
	tws.writeMu.Lock()
	conn := tws.connection
	tws.connection = nil
	tws.writeMu.Unlock()
	if conn != nil {
		conn.Close()
	}
	tws.BaseStreamer.Cancel()
	return nil
}

func (tws *twilioWebsocketStreamer) handleMediaEvent(mediaEvent internal_twilio.TwilioMediaEvent) (*protos.ConversationUserMessage, error) {
	payloadBytes, err := tws.Encoder().DecodeString(mediaEvent.Media.Payload)
	if err != nil {
		tws.Logger.Warn("Failed to decode media payload", "error", err.Error())
		return nil, nil
	}

	var audioRequest *protos.ConversationUserMessage
	tws.WithInputBuffer(func(buf *bytes.Buffer) {
		buf.Write(payloadBytes)
		if buf.Len() >= tws.InputBufferThreshold() {
			audioRequest = tws.CreateVoiceRequest(buf.Bytes())
			buf.Reset()
		}
	})
	if audioRequest == nil {
		return nil, nil
	}
	return audioRequest, nil
}

func (tws *twilioWebsocketStreamer) sendTwilioMessage(
	eventType string,
	mediaData map[string]interface{}) error {
	if tws.connection == nil || tws.streamID == "" {
		return nil
	}
	message := map[string]interface{}{
		"event":     eventType,
		"streamSid": tws.streamID,
	}
	if mediaData != nil {
		message["media"] = mediaData
	}

	twilioMessageJSON, err := json.Marshal(message)
	if err != nil {
		return tws.handleError("Failed to marshal Twilio message", err)
	}

	tws.writeMu.Lock()
	defer tws.writeMu.Unlock()
	if tws.connection == nil {
		return nil
	}
	if err := tws.connection.WriteMessage(websocket.TextMessage, twilioMessageJSON); err != nil {
		return tws.handleError("Failed to send message to Twilio", err)
	}

	return nil
}

func (tws *twilioWebsocketStreamer) handleError(message string, err error) error {
	tws.Logger.Error(message, "error", err.Error())
	return err
}

