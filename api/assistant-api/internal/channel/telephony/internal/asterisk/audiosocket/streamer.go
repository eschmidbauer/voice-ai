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
	outputCtx, outputCancel := context.WithCancel(ctx)

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
	return as.writeFrame(FrameTypeAudio, chunk.Data)
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
	as.PushInputLow(&protos.ConversationEvent{
		Name: "channel",
		Data: map[string]string{
			"type":     "connected",
			"provider": "asterisk_as",
		},
		Time: timestamppb.Now(),
	})
	if as.initialUUID != "" {
		as.PushInput(as.CreateConnectionRequest())
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
				as.PushDisconnection(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
				as.BaseStreamer.Cancel()
				return
			}
			as.PushDisconnection(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
			as.BaseStreamer.Cancel()
			return
		}
		switch frame.Type {
		case FrameTypeUUID:
			as.initialUUID = strings.TrimSpace(string(frame.Payload))
			as.PushInput(as.CreateConnectionRequest())
		case FrameTypeAudio:
			if err := as.audioProcessor.ProcessInputAudio(frame.Payload); err != nil {
				as.Logger.Debug("Failed to process input audio", "error", err.Error())
				continue
			}
			var audioRequest *protos.ConversationUserMessage
			as.WithInputBuffer(func(buf *bytes.Buffer) {
				if buf.Len() > 0 {
					audioRequest = &protos.ConversationUserMessage{
						Message: &protos.ConversationUserMessage_Audio{Audio: buf.Bytes()},
					}
					buf.Reset()
				}
			})
			if audioRequest != nil {
				as.PushInput(audioRequest)
			}
		case FrameTypeSilence:
			// no-op
		case FrameTypeHangup:
			as.PushDisconnection(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
			as.BaseStreamer.Cancel()
			return
		case FrameTypeError:
			as.PushDisconnection(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
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
	case *protos.ConversationDirective:
		if data.GetType() == protos.ConversationDirective_END_CONVERSATION {
			_ = as.writeFrame(FrameTypeHangup, nil)
			return as.close()
		}
	}

	return nil
}

func (as *Streamer) close() error {
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
