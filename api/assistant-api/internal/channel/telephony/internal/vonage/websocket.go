// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_vonage_telephony

import (
	"bytes"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_telephony_base "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/base"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	protos "github.com/rapidaai/protos"
	"github.com/vonage/vonage-go-sdk"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type vonageWebsocketStreamer struct {
	internal_telephony_base.BaseTelephonyStreamer

	connection *websocket.Conn
	writeMu    sync.Mutex
	closed     atomic.Bool
}

// NewVonageWebsocketStreamer creates a Vonage WebSocket streamer.
// Vonage sends linear16 16kHz — same as the internal Rapida format, so no
// resampling is needed (nil source audio config defaults to linear16 16kHz).
func NewVonageWebsocketStreamer(logger commons.Logger, connection *websocket.Conn, cc *callcontext.CallContext, vaultCred *protos.VaultCredential) internal_type.Streamer {
	vng := &vonageWebsocketStreamer{
		BaseTelephonyStreamer: internal_telephony_base.NewBaseTelephonyStreamer(
			logger, cc, vaultCred,
		),
		connection: connection,
	}
	go vng.runWebSocketReader()
	return vng
}

func (vng *vonageWebsocketStreamer) runWebSocketReader() {
	conn := vng.connection
	if conn == nil {
		return
	}
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if msg := vng.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				vng.Input(msg)
			}
			vng.BaseStreamer.Cancel()
			return
		}
		switch messageType {
		case websocket.TextMessage:
			var textEvent map[string]interface{}
			if err := json.Unmarshal(message, &textEvent); err != nil {
				vng.Logger.Error("Failed to unmarshal text event", "error", err.Error())
				continue
			}
			switch textEvent["event"] {
			case "websocket:connected":
				vng.Input(vng.CreateConnectionRequest())
				vng.Input(&protos.ConversationEvent{
					Name: "channel",
					Data: map[string]string{"type": "connected", "provider": "vonage"},
					Time: timestamppb.Now(),
				})
			case "stop":
				if msg := vng.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
					vng.Input(msg)
				}
				vng.BaseStreamer.Cancel()
				return
			default:
				vng.Logger.Debugf("Unhandled event type: %s", textEvent["event"])
			}
		case websocket.BinaryMessage:
			msg, _ := vng.handleMediaEvent(message)
			if msg != nil {
				vng.Input(msg)
			}
		default:
			vng.Logger.Warn("Unhandled message type", "type", messageType)
		}
	}
}

func (vng *vonageWebsocketStreamer) Send(response internal_type.Stream) error {
	if vng.connection == nil {
		return nil
	}
	switch data := response.(type) {
	case *protos.ConversationAssistantMessage:
		switch content := data.Message.(type) {
		case *protos.ConversationAssistantMessage_Audio:
			audioData := content.Audio

			var sendErr error
			vng.WithOutputBuffer(func(buf *bytes.Buffer) {
				buf.Write(audioData)
				vng.writeMu.Lock()
				defer vng.writeMu.Unlock()
				if vng.connection == nil {
					return
				}
				for buf.Len() >= vng.OutputFrameSize() {
					chunk := buf.Next(vng.OutputFrameSize())
					if err := vng.connection.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
						vng.Logger.Error("Failed to send audio chunk", "error", err.Error())
						sendErr = err
						return
					}
				}
				if data.GetCompleted() && buf.Len() > 0 {
					remainingChunk := buf.Bytes()
					if err := vng.connection.WriteMessage(websocket.BinaryMessage, remainingChunk); err != nil {
						vng.Logger.Errorf("Failed to send final audio chunk", "error", err.Error())
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
			vng.ResetOutputBuffer()
			vng.writeMu.Lock()
			if vng.connection != nil {
				if err := vng.connection.WriteMessage(websocket.TextMessage, []byte(`{"action":"clear"}`)); err != nil {
					vng.Logger.Errorf("Error sending clear command:", err)
				}
			}
			vng.writeMu.Unlock()
		}
	case *protos.ConversationToolCall:
		switch data.GetAction() {
		case protos.ToolCallAction_TOOL_CALL_ACTION_END_CONVERSATION:
			if vng.GetConversationUuid() != "" {
				cAuth, err := vonageAuth(vng.VaultCredential())
				if err != nil {
					vng.Logger.Errorf("Error creating Vonage client:", err)
					vng.Cancel()
					return nil
				}
				if _, _, err := vonage.NewVoiceClient(cAuth).Hangup(vng.GetConversationUuid()); err != nil {
					vng.Logger.Errorf("Error ending Vonage call:", err)
					vng.Cancel()
					return nil
				}
			}
			vng.Cancel()
		case protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION:
			vng.Logger.Warnw("Vonage call transfer not yet implemented", "to", data.GetArgs()["to"])
			if data.GetToolId() != "" {
				vng.Input(&protos.ConversationToolCallResult{
					Id:     data.GetId(),
					ToolId: data.GetToolId(), Name: data.GetName(), Action: data.GetAction(),
					Result: map[string]string{"status": "failed", "reason": "transfer not supported for Vonage"},
				})
			}
		}
	}
	return nil
}

func (vng *vonageWebsocketStreamer) handleMediaEvent(message []byte) (*protos.ConversationUserMessage, error) {
	var audioRequest *protos.ConversationUserMessage
	vng.WithInputBuffer(func(buf *bytes.Buffer) {
		buf.Write(message)
		if buf.Len() >= vng.InputBufferThreshold() {
			audioRequest = vng.CreateVoiceRequest(buf.Bytes())
			buf.Reset()
		}
	})
	return audioRequest, nil
}

func (vng *vonageWebsocketStreamer) GetConversationUuid() string {
	return vng.ChannelUUID
}

func (vng *vonageWebsocketStreamer) Cancel() error {
	if !vng.closed.CompareAndSwap(false, true) {
		return nil
	}
	vng.writeMu.Lock()
	conn := vng.connection
	vng.connection = nil
	vng.writeMu.Unlock()
	if conn != nil {
		conn.Close()
	}
	vng.BaseStreamer.Cancel()
	return nil
}

