// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_asterisk_audiosocket

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_asterisk "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/asterisk/internal"
	internal_telephony_base "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/base"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Streamer implements AudioSocket media streaming over TCP.
type Streamer struct {
	internal_telephony_base.BaseTelephonyStreamer

	conn           net.Conn
	reader         *bufio.Reader
	writer         *bufio.Writer
	writeMu        sync.Mutex
	audioProcessor *internal_asterisk.AudioProcessor
	closed         atomic.Bool

	ctx          context.Context
	cancel       context.CancelFunc
	outputCtx    context.Context
	outputCancel context.CancelFunc

	initialUUID string
}

// NewStreamer creates a new AudioSocket streamer.
// initialUUID is the contextId already read from the first UUID frame by the AudioSocket
// engine -- when set, the streamer emits ConversationInitialization on the first Recv()
// without waiting for another UUID frame from the wire.
func NewStreamer(
	logger commons.Logger,
	conn net.Conn,
	reader *bufio.Reader,
	writer *bufio.Writer,
	cc *callcontext.CallContext,
	vaultCred *protos.VaultCredential,
) (internal_type.Streamer, error) {
	audioProcessor, err := internal_asterisk.NewAudioProcessor(logger, internal_asterisk.AudioProcessorConfig{
		AsteriskConfig:   internal_audio.NewLinear8khzMonoAudioConfig(),
		DownstreamConfig: internal_audio.NewLinear16khzMonoAudioConfig(),
		SilenceByte:      0x00, // SLIN silence
		FrameSize:        320,  // 20ms at 8kHz 16-bit
	})
	if err != nil {
		return nil, err
	}

	if reader == nil {
		reader = bufio.NewReader(conn)
	}
	if writer == nil {
		writer = bufio.NewWriter(conn)
	}

	ctx, cancel := context.WithCancel(context.Background())
	outputCtx, outputCancel := context.WithCancel(context.Background())

	as := &Streamer{
		BaseTelephonyStreamer: internal_telephony_base.NewBaseTelephonyStreamer(
			logger, cc, vaultCred,
		),
		conn:           conn,
		reader:         reader,
		writer:         writer,
		audioProcessor: audioProcessor,
		ctx:            ctx,
		cancel:         cancel,
		outputCtx:      outputCtx,
		outputCancel:   outputCancel,
		initialUUID:    cc.ContextID,
	}

	audioProcessor.SetInputAudioCallback(as.sendProcessedInputAudio)
	audioProcessor.SetOutputChunkCallback(as.sendAudioChunk)
	go audioProcessor.RunOutputSender(as.outputCtx)
	go as.runFrameReader()
	return as, nil
}

func (as *Streamer) sendProcessedInputAudio(audio []byte) {
	as.WithInputBuffer(func(buf *bytes.Buffer) {
		buf.Write(audio)
	})
}

func (as *Streamer) sendAudioChunk(chunk *internal_asterisk.AudioChunk) error {
	if as.conn == nil {
		return nil
	}
	if err := as.writeFrame(FrameTypeAudio, chunk.Data); err != nil {
		// Connection dead — stop output sender
		as.outputCancel()
		return err
	}
	return nil
}

func (as *Streamer) writeFrame(frameType byte, payload []byte) error {
	as.writeMu.Lock()
	defer as.writeMu.Unlock()

	if err := WriteFrame(as.writer, frameType, payload); err != nil {
		return err
	}
	return as.writer.Flush()
}

func (as *Streamer) Context() context.Context {
	return as.ctx
}

func (as *Streamer) runFrameReader() {
	as.Input(&protos.ConversationEvent{
		Name: "channel",
		Data: map[string]string{
			"type":     "connected",
			"provider": "asterisk_as",
		},
		Time: timestamppb.Now(),
	})
	if as.initialUUID != "" {
		as.Input(as.CreateConnectionRequest())
	}
	for {
		select {
		case <-as.ctx.Done():
			return
		default:
		}
		frame, err := ReadFrame(as.reader)
		if err != nil {
			if err == io.EOF {
				if msg := as.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
					as.Input(msg)
				}
				as.BaseStreamer.Cancel()
				return
			}
			if msg := as.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				as.Input(msg)
			}
			as.BaseStreamer.Cancel()
			return
		}
		switch frame.Type {
		case FrameTypeUUID:
			if as.initialUUID == "" {
				as.initialUUID = strings.TrimSpace(string(frame.Payload))
				as.Input(as.CreateConnectionRequest())
			}
		case FrameTypeAudio:
			if err := as.audioProcessor.ProcessInputAudio(frame.Payload); err != nil {
				as.Logger.Debug("Failed to process input audio", "error", err.Error())
				continue
			}
			var audioRequest *protos.ConversationUserMessage
			as.WithInputBuffer(func(buf *bytes.Buffer) {
				if buf.Len() > 0 {
					audioRequest = as.CreateVoiceRequest(buf.Bytes())
					buf.Reset()
				}
			})
			if audioRequest != nil {
				as.Input(audioRequest)
			}
		case FrameTypeSilence:
			// no-op
		case FrameTypeHangup:
			if msg := as.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				as.Input(msg)
			}
			as.BaseStreamer.Cancel()
			return
		case FrameTypeError:
			if msg := as.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); msg != nil {
				as.Input(msg)
			}
			as.BaseStreamer.Cancel()
			return
		}
	}
}

func (as *Streamer) Send(response internal_type.Stream) error {
	switch data := response.(type) {
	case *protos.ConversationAssistantMessage:
		switch content := data.GetMessage().(type) {
		case *protos.ConversationAssistantMessage_Audio:
			if err := as.audioProcessor.ProcessOutputAudio(content.Audio); err != nil {
				return err
			}
		}
	case *protos.ConversationInterruption:
		if data.GetType() == protos.ConversationInterruption_INTERRUPTION_TYPE_WORD {
			as.audioProcessor.ClearOutputBuffer()
		}
	case *protos.ConversationToolCall:
		switch data.GetAction() {
		case protos.ToolCallAction_TOOL_CALL_ACTION_END_CONVERSATION:
			_ = as.writeFrame(FrameTypeHangup, nil)
			return as.close()
		case protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION:
			as.Logger.Warnw("Call transfer not supported for AudioSocket")
			if data.GetToolId() != "" {
				as.Input(&protos.ConversationToolCallResult{
					Id:     data.GetId(),
					ToolId: data.GetToolId(), Name: data.GetName(), Action: data.GetAction(),
					Result: map[string]string{"status": "failed", "reason": "transfer not supported for AudioSocket"},
				})
			}
		}
	}

	return nil
}

func (as *Streamer) close() error {
	if !as.closed.CompareAndSwap(false, true) {
		return nil
	}
	if as.outputCancel != nil {
		as.outputCancel()
	}
	if as.cancel != nil {
		as.cancel()
	}
	as.BaseStreamer.Cancel()
	if as.conn != nil {
		if as.writer != nil {
			_ = as.writer.Flush()
		}
		_ = as.conn.Close()
		as.conn = nil
	}
	return nil
}
