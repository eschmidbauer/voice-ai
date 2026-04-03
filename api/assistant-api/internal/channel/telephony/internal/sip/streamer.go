// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_sip_telephony

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_telephony_base "github.com/rapidaai/api/assistant-api/internal/channel/telephony/internal/base"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"github.com/zaf/g711"
)

var (
	rapida16kConfig = internal_audio.NewLinear16khzMonoAudioConfig()
	mulaw8kConfig   = internal_audio.NewMulaw8khzMonoAudioConfig()
)

// Streamer implements the TelephonyStreamer interface using native SIP/RTP.
type Streamer struct {
	internal_telephony_base.BaseTelephonyStreamer

	mu     sync.RWMutex
	closed atomic.Bool

	config     *sip_infra.Config
	session    *sip_infra.Session
	rtpHandler *sip_infra.RTPHandler

	codec *sip_infra.Codec
	ctx   context.Context
	cancel context.CancelFunc
}

// NewStreamer creates a SIP streamer that reuses an existing session's RTP handler.
func NewStreamer(ctx context.Context,
	config *sip_infra.Config,
	logger commons.Logger,
	sipSession *sip_infra.Session,
	cc *callcontext.CallContext,
	vaultCred *protos.VaultCredential,
) (internal_type.Streamer, error) {
	if sipSession == nil {
		return nil, fmt.Errorf("SIP session is required — standalone server mode is not supported")
	}
	streamerCtx, cancel := context.WithCancel(ctx)

	// Default codec — overridden below when an existing session carries a
	// negotiated codec.
	pcmu := sip_infra.CodecPCMU
	codec := &pcmu

	s := &Streamer{
		BaseTelephonyStreamer: internal_telephony_base.NewBaseTelephonyStreamer(
			logger, cc, vaultCred,
			internal_telephony_base.WithSourceAudioConfig(internal_audio.NewMulaw8khzMonoAudioConfig()),
		),
		config: config,
		codec:  codec,
		ctx:    streamerCtx,
		cancel: cancel,
	}

	// Bridge SIP context to BaseStreamer so Recv() returns io.EOF on session end.
	go func() {
		<-streamerCtx.Done()
		s.BaseStreamer.Cancel()
	}()

	rtpHandler := sipSession.GetRTPHandler()
	if rtpHandler == nil {
		cancel()
		return nil, sip_infra.NewSIPError("NewStreamer", sipSession.GetCallID(), "session has no RTP handler", sip_infra.ErrRTPNotInitialized)
	}

	if negotiated := sipSession.GetNegotiatedCodec(); negotiated != nil {
		s.codec = negotiated
	}

	s.session = sipSession
	s.rtpHandler = rtpHandler

	go s.forwardIncomingAudio()
	go s.runRTPWriter()
	s.PushInput(s.CreateConnectionRequest())

	localIP, localPort := rtpHandler.LocalAddr()
	logger.Infow("SIP streamer created",
		"call_id", sipSession.GetCallID(),
		"codec", s.codec.Name,
		"rtp_port", localPort,
		"local_ip", localIP)

	return s, nil
}

// forwardIncomingAudio reads RTP packets, transcodes A-law→µ-law when PCMA
// is negotiated, and pushes audio to InputCh via PushInput.
func (s *Streamer) forwardIncomingAudio() {
	s.mu.RLock()
	rtpHandler := s.rtpHandler
	s.mu.RUnlock()
	if rtpHandler == nil {
		s.Logger.Error("forwardIncomingAudio: RTP handler is nil")
		return
	}
	select {
	case <-s.ctx.Done():
		s.Logger.Error("forwardIncomingAudio: Context already cancelled at start!", "err", s.ctx.Err())
		return
	default:
	}
	bufferThreshold := s.InputBufferThreshold()
	for {
		select {
		case <-s.ctx.Done():
			return
		case audioData, ok := <-rtpHandler.AudioIn():
			if !ok {
				return
			}
			if codec := rtpHandler.GetCodec(); codec != nil && codec.Name == "PCMA" {
				audioData = g711.Alaw2Ulaw(audioData)
			}
			var audioReq *protos.ConversationUserMessage
			s.WithInputBuffer(func(buf *bytes.Buffer) {
				buf.Write(audioData)
				if buf.Len() >= bufferThreshold {
					data := make([]byte, buf.Len())
					copy(data, buf.Bytes())
					buf.Reset()
					audioReq = s.CreateVoiceRequest(data)
				}
			})
			if audioReq != nil {
				s.PushInput(audioReq)
			}
		}
	}
}

func (s *Streamer) Context() context.Context {
	return s.ctx
}

func (s *Streamer) Send(response internal_type.Stream) error {
	if s.closed.Load() {
		return sip_infra.ErrSessionClosed
	}
	switch data := response.(type) {
	case *protos.ConversationAssistantMessage:
		switch content := data.Message.(type) {
		case *protos.ConversationAssistantMessage_Audio:
			return s.sendAudio(content.Audio)
		}
	case *protos.ConversationInterruption:
		if data.Type == protos.ConversationInterruption_INTERRUPTION_TYPE_WORD {
			return s.handleInterruption()
		}
	case *protos.ConversationDirective:
		if data.GetType() == protos.ConversationDirective_END_CONVERSATION {
			return s.Close()
		}
	}
	return nil
}

func (s *Streamer) sendAudio(audioData []byte) error {
	s.mu.RLock()
	rtpHandler := s.rtpHandler
	s.mu.RUnlock()

	if rtpHandler == nil || !rtpHandler.IsRunning() {
		return sip_infra.ErrRTPNotInitialized
	}

	codec := rtpHandler.GetCodec()

	outData, err := s.Resampler().Resample(audioData, rapida16kConfig, mulaw8kConfig)
	if err != nil {
		s.Logger.Error("sendAudio: failed to resample audio", "error", err)
		return err
	}

	if codec != nil && codec.Name == "PCMA" {
		outData = mulawToAlaw(outData)
	}

	s.BufferAndSendOutput(outData)
	return nil
}

// runRTPWriter paces 20ms audio frames from OutputCh to the RTP handler at real-time rate.
func (s *Streamer) runRTPWriter() {
	const pacingInterval = 20 * time.Millisecond
	ticker := time.NewTicker(pacingInterval)
	defer ticker.Stop()

	// pendingAudio holds 20ms PCM frames waiting for the next tick.
	var pendingAudio [][]byte

	for {
		select {
		case <-s.ctx.Done():
			return

		case <-s.FlushAudioCh:
			pendingAudio = pendingAudio[:0]
			s.mu.RLock()
			rtpHandler := s.rtpHandler
			s.mu.RUnlock()
			if rtpHandler != nil {
				rtpHandler.FlushAudioOut()
			}

		case <-ticker.C:
			if len(pendingAudio) > 0 {
				s.mu.RLock()
				rtpHandler := s.rtpHandler
				s.mu.RUnlock()

				if rtpHandler != nil && rtpHandler.IsRunning() {
					select {
					case rtpHandler.AudioOut() <- pendingAudio[0]:
					case <-s.ctx.Done():
						return
					default:
						continue
					}
				}
				pendingAudio = pendingAudio[1:]
			}

		case msg := <-s.OutputCh:
			if m, ok := msg.(*protos.ConversationAssistantMessage); ok {
				if audio, ok := m.Message.(*protos.ConversationAssistantMessage_Audio); ok {
					pendingAudio = append(pendingAudio, audio.Audio)
				}
			}
		}
	}
}

func (s *Streamer) handleInterruption() error {
	s.ClearOutputBuffer()
	return nil
}

// Close sends SIP BYE to the remote party, then releases all local resources.
func (s *Streamer) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	s.cancel()
	s.BaseStreamer.Cancel()

	s.mu.Lock()
	rtpHandler := s.rtpHandler
	session := s.session
	s.rtpHandler = nil
	s.session = nil
	s.mu.Unlock()

	s.ResetInputBuffer()

	if session != nil {
		session.Disconnect()
	}
	if rtpHandler != nil {
		if err := rtpHandler.Stop(); err != nil {
			s.Logger.Warn("Error stopping RTP handler", "error", err)
		}
	}
	if session != nil {
		session.End()
	}

	s.Logger.Infow("SIP streamer closed")
	return nil
}

func mulawToAlaw(in []byte) []byte {
	return g711.EncodeAlaw(g711.DecodeUlaw(in))
}
