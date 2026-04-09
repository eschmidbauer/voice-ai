// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_asterisk_websocket

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_asterisk "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/asterisk/internal"
	internal_telephony_base "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/base"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type asteriskWebsocketStreamer struct {
	internal_telephony_base.BaseTelephonyStreamer

	audioProcessor *internal_asterisk.AudioProcessor
	connection     *websocket.Conn
	writeMu        sync.Mutex // guards all writes to connection (gorilla WS is not concurrent-write safe)
	closed         atomic.Bool
	channelName    string

	outputSenderStarted bool
	outputSenderMu      sync.Mutex
	audioCtx            context.Context
	audioCancel         context.CancelFunc

	mediaBuffering bool
	mediaBufferMu  sync.Mutex
}

// NewAsteriskWebsocketStreamer creates a new Asterisk WebSocket streamer.
func NewAsteriskWebsocketStreamer(
	logger commons.Logger,
	connection *websocket.Conn,
	cc *callcontext.CallContext,
	vaultCred *protos.VaultCredential,
) (internal_type.Streamer, error) {
	audioProcessor, err := internal_asterisk.NewAudioProcessor(logger, internal_asterisk.AudioProcessorConfig{
		AsteriskConfig:   internal_audio.NewMulaw8khzMonoAudioConfig(),
		DownstreamConfig: internal_audio.NewLinear16khzMonoAudioConfig(),
		SilenceByte:      0xFF, // mu-law silence
		FrameSize:        160,  // 20ms at 8kHz 8-bit
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create audio processor: %w", err)
	}

	aws := &asteriskWebsocketStreamer{
		BaseTelephonyStreamer: internal_telephony_base.NewBaseTelephonyStreamer(
			logger, cc, vaultCred,
		),
		audioProcessor: audioProcessor,
		connection:     connection,
	}

	audioProcessor.SetInputAudioCallback(aws.sendProcessedInputAudio)
	audioProcessor.SetOutputChunkCallback(aws.sendAudioChunk)

	go aws.runWebSocketReader()
	return aws, nil
}

func (aws *asteriskWebsocketStreamer) sendProcessedInputAudio(audio []byte) {
	aws.WithInputBuffer(func(buf *bytes.Buffer) {
		buf.Write(audio)
	})
}

func (aws *asteriskWebsocketStreamer) sendAudioChunk(chunk *internal_asterisk.AudioChunk) error {
	if aws.connection == nil {
		return nil
	}
	aws.writeMu.Lock()
	defer aws.writeMu.Unlock()
	return aws.connection.WriteMessage(websocket.BinaryMessage, chunk.Data)
}

func (aws *asteriskWebsocketStreamer) runWebSocketReader() {
	conn := aws.connection
	if conn == nil {
		return
	}
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			aws.PushDisconnection(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
			aws.BaseStreamer.Cancel()
			return
		}
		switch messageType {
		case websocket.BinaryMessage:
			msg, _ := aws.handleAudioData(message)
			if msg != nil {
				aws.PushInput(msg)
			}
		case websocket.TextMessage:
			event, err := internal_asterisk.ParseAsteriskEvent(string(message))
			if err != nil {
				aws.Logger.Warn("Failed to parse Asterisk event", "error", err.Error(), "message", message)
				continue
			}
			switch event.Event {
			case "MEDIA_START":
				aws.channelName = event.Channel
				aws.ChannelUUID = event.Channel // propagate to base streamer for client.provider_call_id
				aws.Logger.Info("Asterisk media started", "channel", aws.channelName, "optimal_frame_size", event.OptimalFrameSize)
				if event.OptimalFrameSize > 0 {
					aws.audioProcessor.SetOptimalFrameSize(event.OptimalFrameSize)
				}
				aws.startOutputSender()
				aws.PushInput(aws.CreateConnectionRequest())
				aws.PushInputLow(&protos.ConversationEvent{
					Name: "channel",
					Data: map[string]string{"type": "media_started", "provider": "asterisk_ws", "channel_name": aws.channelName},
					Time: timestamppb.Now(),
				})
			case "MEDIA_STOP":
				aws.Logger.Info("Asterisk media stopped")
				aws.stopAudioProcessing()
				aws.PushDisconnection(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
				aws.Cancel()
				return
			case "MEDIA_XON":
				aws.audioProcessor.SetXON()
				aws.PushInputLow(&protos.ConversationEvent{
					Name: "channel",
					Data: map[string]string{"type": "flow_control", "provider": "asterisk_ws", "state": "xon"},
					Time: timestamppb.Now(),
				})
			case "MEDIA_XOFF":
				aws.audioProcessor.SetXOFF()
				aws.PushInputLow(&protos.ConversationEvent{
					Name: "channel",
					Data: map[string]string{"type": "flow_control", "provider": "asterisk_ws", "state": "xoff"},
					Time: timestamppb.Now(),
				})
			case "MEDIA_BUFFERING_COMPLETED":
				aws.setMediaBuffering(false)
			default:
				if event.Command != "" {
					aws.Logger.Debug("Received Asterisk command response", "command", event.Command)
				} else if event.RawMessage != "" {
					aws.Logger.Debug("Received unhandled Asterisk message", "message", event.RawMessage)
				}
			}
		case websocket.CloseMessage:
			aws.PushDisconnection(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
			aws.BaseStreamer.Cancel()
			return
		default:
			aws.Logger.Warn("Received unsupported WebSocket message type", "type", messageType)
		}
	}
}

func (aws *asteriskWebsocketStreamer) handleAudioData(audio []byte) (*protos.ConversationUserMessage, error) {
	if err := aws.audioProcessor.ProcessInputAudio(audio); err != nil {
		aws.Logger.Debug("Failed to process input audio", "error", err.Error())
		return nil, nil
	}

	var audioRequest *protos.ConversationUserMessage
	aws.WithInputBuffer(func(buf *bytes.Buffer) {
		if buf.Len() > 0 {
			audioRequest = aws.CreateVoiceRequest(buf.Bytes())
			buf.Reset()
		}
	})

	return audioRequest, nil
}

func (aws *asteriskWebsocketStreamer) Send(response internal_type.Stream) error {
	switch data := response.(type) {
	case *protos.ConversationAssistantMessage:
		switch content := data.Message.(type) {
		case *protos.ConversationAssistantMessage_Audio:
			if err := aws.audioProcessor.ProcessOutputAudio(content.Audio); err != nil {
				aws.Logger.Error("Failed to process output audio", "error", err.Error())
				return err
			}
		}

	case *protos.ConversationInterruption:
		if data.Type == protos.ConversationInterruption_INTERRUPTION_TYPE_WORD {
			aws.audioProcessor.ClearOutputBuffer()
			if aws.isMediaBuffering() {
				aws.sendCommand("STOP_MEDIA_BUFFERING")
			}
		}

	case *protos.ConversationDirective:
		if data.GetType() == protos.ConversationDirective_END_CONVERSATION {
			aws.stopAudioProcessing()
			if err := aws.sendCommand("HANGUP"); err != nil {
				aws.Logger.Warn("Failed to send HANGUP via WebSocket, trying ARI API", "error", err)
				if aws.channelName != "" {
					if err := aws.hangupViaARI(); err != nil {
						aws.Logger.Error("Failed to hangup via ARI API", "error", err)
					}
				}
			}
			if err := aws.Cancel(); err != nil {
				aws.Logger.Errorf("Error disconnecting:", err)
			}
		}
	}

	return nil
}

func (aws *asteriskWebsocketStreamer) stopAudioProcessing() {
	aws.outputSenderMu.Lock()
	if aws.audioCancel != nil {
		aws.audioCancel()
		aws.audioCancel = nil
	}
	aws.outputSenderMu.Unlock()
}

func (aws *asteriskWebsocketStreamer) startOutputSender() {
	aws.outputSenderMu.Lock()
	defer aws.outputSenderMu.Unlock()

	if aws.outputSenderStarted {
		return
	}

	aws.audioCtx, aws.audioCancel = context.WithCancel(aws.BaseTelephonyStreamer.Context())
	aws.outputSenderStarted = true
	go aws.audioProcessor.RunOutputSender(aws.audioCtx)
}

func (aws *asteriskWebsocketStreamer) sendCommand(command string) error {
	if aws.connection == nil {
		return nil
	}
	aws.writeMu.Lock()
	defer aws.writeMu.Unlock()
	return aws.connection.WriteMessage(websocket.TextMessage, []byte(command))
}

func (aws *asteriskWebsocketStreamer) setMediaBuffering(buffering bool) {
	aws.mediaBufferMu.Lock()
	aws.mediaBuffering = buffering
	aws.mediaBufferMu.Unlock()
}

func (aws *asteriskWebsocketStreamer) isMediaBuffering() bool {
	aws.mediaBufferMu.Lock()
	defer aws.mediaBufferMu.Unlock()
	return aws.mediaBuffering
}

func (aws *asteriskWebsocketStreamer) hangupViaARI() error {
	vaultCredential := aws.VaultCredential()
	if vaultCredential == nil || vaultCredential.GetValue() == nil {
		return fmt.Errorf("vault credential is nil")
	}

	credMap := vaultCredential.GetValue().AsMap()

	ariURL, _ := credMap["ari_url"].(string)
	ariURL = fmt.Sprintf("%s/ari/channels/%s", ariURL, aws.channelName)
	user, _ := credMap["ari_user"].(string)
	password, _ := credMap["ari_password"].(string)

	req, err := http.NewRequest("DELETE", ariURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(user, password)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("ARI API returned status: %d", resp.StatusCode)
	}

	aws.Logger.Info("Successfully hung up call via ARI API", "channel", aws.channelName)
	return nil
}

func (aws *asteriskWebsocketStreamer) Cancel() error {
	if !aws.closed.CompareAndSwap(false, true) {
		return nil
	}
	aws.stopAudioProcessing()
	aws.writeMu.Lock()
	conn := aws.connection
	aws.connection = nil
	aws.writeMu.Unlock()
	if conn != nil {
		conn.Close()
	}
	aws.BaseStreamer.Cancel()
	return nil
}
