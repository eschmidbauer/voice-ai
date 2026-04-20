// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_exotel_telephony

import (
	"bytes"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_telephony_base "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/base"
	internal_exotel "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/exotel/internal"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var exotelLinear8kConfig = internal_audio.NewLinear8khzMonoAudioConfig()

type exotelWebsocketStreamer struct {
	internal_telephony_base.BaseTelephonyStreamer

	connection *websocket.Conn
	writeMu    sync.Mutex
	closed     atomic.Bool
	streamID   string
}

func NewExotelWebsocketStreamer(logger commons.Logger, connection *websocket.Conn, cc *callcontext.CallContext, vaultCred *protos.VaultCredential,
) internal_type.Streamer {
	exotel := &exotelWebsocketStreamer{
		BaseTelephonyStreamer: internal_telephony_base.NewBaseTelephonyStreamer(
			logger, cc, vaultCred,
			internal_telephony_base.WithSourceAudioConfig(exotelLinear8kConfig),
		),
		streamID:   "",
		connection: connection,
	}
	go exotel.runWebSocketReader()
	return exotel
}

func (exotel *exotelWebsocketStreamer) runWebSocketReader() {
	conn := exotel.connection
	if conn == nil {
		return
	}
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if msg := exotel.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				exotel.Input(msg)
			}
			exotel.BaseStreamer.Cancel()
			return
		}
		var mediaEvent internal_exotel.ExotelMediaEvent
		if err := json.Unmarshal(message, &mediaEvent); err != nil {
			exotel.Logger.Error("Failed to unmarshal Exotel media event", "error", err.Error())
			continue
		}
		switch mediaEvent.Event {
		case "connected":
			exotel.Input(exotel.CreateConnectionRequest())
			exotel.Input(&protos.ConversationEvent{
				Name: "channel",
				Data: map[string]string{"type": "connected", "provider": "exotel"},
				Time: timestamppb.Now(),
			})
		case "start":
			exotel.handleStartEvent(mediaEvent)
			exotel.Input(&protos.ConversationEvent{
				Name: "channel",
				Data: map[string]string{"type": "stream_started", "provider": "exotel", "stream_id": exotel.streamID},
				Time: timestamppb.Now(),
			})
		case "media":
			msg, _ := exotel.handleMediaEvent(mediaEvent)
			if msg != nil {
				exotel.Input(msg)
			}
		case "dtmf":
			exotel.Input(&protos.ConversationEvent{
				Name: "channel",
				Data: map[string]string{"type": "dtmf", "provider": "exotel"},
				Time: timestamppb.Now(),
			})
		case "stop":
			if msg := exotel.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				exotel.Input(msg)
			}
			exotel.Cancel()
			return
		default:
			exotel.Logger.Warn("Unhandled Exotel event", "event", mediaEvent.Event)
		}
	}
}

func (exotel *exotelWebsocketStreamer) Send(response internal_type.Stream) error {
	switch data := response.(type) {
	case *protos.ConversationAssistantMessage:
		switch content := data.Message.(type) {
		case *protos.ConversationAssistantMessage_Audio:
			audioData, err := exotel.Resampler().Resample(content.Audio, internal_audio.RAPIDA_INTERNAL_AUDIO_CONFIG, exotelLinear8kConfig)
			if err != nil {
				exotel.Logger.Warnw("Failed to resample output audio to linear16 8kHz, forwarding raw bytes",
					"error", err.Error(),
				)
				audioData = content.Audio
			}

			var sendErr error
			exotel.WithOutputBuffer(func(buf *bytes.Buffer) {
				buf.Write(audioData)
				for buf.Len() >= exotel.OutputFrameSize() && exotel.streamID != "" {
					chunk := buf.Next(exotel.OutputFrameSize())
					if err := exotel.sendExotelMessage("media", map[string]interface{}{
						"payload": exotel.Encoder().EncodeToString(chunk),
					}); err != nil {
						exotel.Logger.Error("Failed to send audio chunk", "error", err.Error())
						sendErr = err
						return
					}
				}
				if data.GetCompleted() && buf.Len() > 0 {
					remainingChunk := buf.Bytes()
					if err := exotel.sendExotelMessage("media", map[string]interface{}{
						"payload": exotel.Encoder().EncodeToString(remainingChunk),
					}); err != nil {
						exotel.Logger.Errorf("Failed to send final audio chunk", "error", err.Error())
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
			exotel.ResetOutputBuffer()
			if err := exotel.sendExotelMessage("clear", nil); err != nil {
				exotel.Logger.Errorf("Error sending clear command:", err)
			}
		}
	case *protos.ConversationToolCall:
		switch data.GetAction() {
		case protos.ToolCallAction_TOOL_CALL_ACTION_END_CONVERSATION:
			exotel.Cancel()
		case protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION:
			exotel.Logger.Warnw("Call transfer not supported for Exotel")
			if data.GetToolId() != "" {
				exotel.Input(&protos.ConversationToolCallResult{
					Id:     data.GetId(),
					ToolId: data.GetToolId(), Name: data.GetName(), Action: data.GetAction(),
					Result: map[string]string{"status": "failed", "reason": "transfer not supported for Exotel"},
				})
			}
		}
	}
	return nil
}

func (exotel *exotelWebsocketStreamer) handleStartEvent(mediaEvent internal_exotel.ExotelMediaEvent) {
	exotel.streamID = mediaEvent.StreamSid
}

func (exotel *exotelWebsocketStreamer) handleMediaEvent(mediaEvent internal_exotel.ExotelMediaEvent) (*protos.ConversationUserMessage, error) {
	payloadBytes, err := exotel.Encoder().DecodeString(mediaEvent.Media.Payload)
	if err != nil {
		exotel.Logger.Warn("Failed to decode media payload", "error", err.Error())
		return nil, nil
	}

	var audioRequest *protos.ConversationUserMessage
	exotel.WithInputBuffer(func(buf *bytes.Buffer) {
		buf.Write(payloadBytes)
		if buf.Len() >= exotel.InputBufferThreshold() {
			audioRequest = exotel.CreateVoiceRequest(buf.Bytes())
			buf.Reset()
		}
	})
	return audioRequest, nil
}

func (exotel *exotelWebsocketStreamer) sendExotelMessage(eventType string, mediaData map[string]interface{}) error {
	if exotel.streamID == "" {
		return nil
	}
	message := map[string]interface{}{
		"event":     eventType,
		"streamSid": exotel.streamID,
	}
	if mediaData != nil {
		message["media"] = mediaData
	}
	exotelMessageJSON, err := json.Marshal(message)
	if err != nil {
		return exotel.handleError("Failed to marshal Exotel message", err)
	}
	exotel.writeMu.Lock()
	defer exotel.writeMu.Unlock()
	if exotel.connection == nil {
		return nil
	}
	if err := exotel.connection.WriteMessage(websocket.TextMessage, exotelMessageJSON); err != nil {
		return exotel.handleError("Failed to send message to Exotel", err)
	}
	return nil
}

func (exotel *exotelWebsocketStreamer) handleError(message string, err error) error {
	exotel.Logger.Error(message, "error", err.Error())
	return err
}

func (exotel *exotelWebsocketStreamer) Cancel() error {
	if !exotel.closed.CompareAndSwap(false, true) {
		return nil
	}
	exotel.writeMu.Lock()
	conn := exotel.connection
	exotel.connection = nil
	exotel.writeMu.Unlock()
	if conn != nil {
		conn.Close()
	}
	exotel.BaseStreamer.Cancel()
	return nil
}
