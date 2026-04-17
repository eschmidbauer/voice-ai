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
	rapida_utils "github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"google.golang.org/protobuf/types/known/anypb"
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

	session    *sip_infra.Session
	rtpHandler *sip_infra.RTPHandler

	transferring        atomic.Bool
	ringbackCancel      context.CancelFunc
	onTransferInitiated func(target string)

	ctx    context.Context
	cancel context.CancelFunc
}

// NewStreamer creates a SIP streamer that reuses an existing session's RTP handler.
func NewStreamer(ctx context.Context,
	logger commons.Logger,
	sipSession *sip_infra.Session,
	cc *callcontext.CallContext,
	vaultCred *protos.VaultCredential,
) (internal_type.Streamer, error) {
	if sipSession == nil {
		return nil, fmt.Errorf("SIP session is required — standalone server mode is not supported")
	}
	streamerCtx, cancel := context.WithCancel(ctx)

	s := &Streamer{
		BaseTelephonyStreamer: internal_telephony_base.NewBaseTelephonyStreamer(
			logger, cc, vaultCred,
			internal_telephony_base.WithSourceAudioConfig(internal_audio.NewMulaw8khzMonoAudioConfig()),
		),
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

	s.session = sipSession
	s.rtpHandler = rtpHandler

	go s.forwardIncomingAudio()
	go s.runRTPWriter()
	s.PushInput(s.CreateConnectionRequest())

	localIP, localPort := rtpHandler.LocalAddr()
	codecName := "PCMU"
	if negotiated := sipSession.GetNegotiatedCodec(); negotiated != nil {
		codecName = negotiated.Name
	}
	logger.Infow("SIP streamer created",
		"call_id", sipSession.GetCallID(),
		"codec", codecName,
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
			if s.transferring.Load() {
				continue
			}
			if codec := rtpHandler.GetCodec(); codec != nil && codec.Name == "PCMA" {
				audioData = internal_audio.AlawToUlaw(audioData)
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
		switch data.GetType() {
		case protos.ConversationDirective_END_CONVERSATION:
			return s.Close()
		case protos.ConversationDirective_TRANSFER_CONVERSATION:
			to := s.extractTransferTarget(data.GetArgs())
			if to == "" {
				s.Logger.Warnw("Transfer directive missing 'to' target")
				return nil
			}
			s.mu.RLock()
			if s.session != nil {
				s.session.SetMetadata(sip_infra.MetadataBridgeTransferTarget, to)
			}
			s.mu.RUnlock()
			s.EnterTransferMode(to)
			return nil
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
		outData = s.mulawToAlaw(outData)
	}

	s.BufferAndSendOutput(outData)
	return nil
}

// runRTPWriter paces 20ms audio frames from OutputCh to the RTP handler at real-time rate.
func (s *Streamer) runRTPWriter() {
	const pacingInterval = 20 * time.Millisecond
	ticker := time.NewTicker(pacingInterval)
	defer ticker.Stop()
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

func (s *Streamer) EnterTransferMode(target string) {
	if s.transferring.Load() {
		return
	}
	s.transferring.Store(true)
	s.ClearOutputBuffer()

	s.mu.RLock()
	session := s.session
	callback := s.onTransferInitiated
	s.mu.RUnlock()

	if session != nil {
		session.SetState(sip_infra.CallStateTransferring)
	}

	ringbackCtx, ringbackCancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.ringbackCancel = ringbackCancel
	s.mu.Unlock()
	go s.playRingback(ringbackCtx)

	if callback != nil {
		callback(target)
	}
}

func (s *Streamer) ExitTransferMode() {
	if !s.transferring.Load() {
		return
	}

	s.mu.RLock()
	cancelFn := s.ringbackCancel
	session := s.session
	s.mu.RUnlock()

	if cancelFn != nil {
		cancelFn()
	}
	if session != nil {
		session.SetState(sip_infra.CallStateConnected)
	}

	s.transferring.Store(false)
	s.Logger.Infow("Transfer mode: exited, AI resuming")
}

func (s *Streamer) IsTransferring() bool {
	return s.transferring.Load()
}

func (s *Streamer) SetOnTransferInitiated(fn func(target string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTransferInitiated = fn
}

func (s *Streamer) playRingback(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	offset := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var frame []byte
			frame, offset = internal_audio.GenerateRingbackFrame(offset)
			_ = s.sendAudio(frame)
		}
	}
}

func (s *Streamer) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	s.cancel()
	s.BaseStreamer.Cancel()
	s.ResetInputBuffer()

	s.mu.RLock()
	session := s.session
	s.mu.RUnlock()

	if s.transferring.Load() {
		return nil
	}

	if session != nil {
		session.End()
	}

	s.Logger.Infow("SIP streamer closed")
	return nil
}

func (s *Streamer) mulawToAlaw(in []byte) []byte {
	return internal_audio.UlawToAlaw(in)
}

// extractTransferTarget reads the "to" field from a ConversationDirective's Args map.
func (s *Streamer) extractTransferTarget(args map[string]*anypb.Any) string {
	if args == nil {
		return ""
	}
	iface, err := rapida_utils.AnyMapToInterfaceMap(args)
	if err != nil {
		return ""
	}
	if to, ok := iface["to"].(string); ok {
		return to
	}
	return ""
}
