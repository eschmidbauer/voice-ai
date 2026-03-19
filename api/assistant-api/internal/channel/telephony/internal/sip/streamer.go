// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_sip_telephony

import (
	"bytes"
	"context"
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
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Streamer constants
const (
	// RTP packet interval in milliseconds
	packetIntervalMs = 20
)

// Audio format configurations for resampling
var (
	// RAPIDA_AUDIO_CONFIG is the internal Rapida audio format (LINEAR16 16kHz mono).
	// TTS engines produce audio in this format.
	RAPIDA_AUDIO_CONFIG = internal_audio.NewLinear16khzMonoAudioConfig()

	// MULAW_8K_AUDIO_CONFIG is the SIP/RTP native audio format (µ-law 8kHz mono).
	// Audio must be converted to this format before sending via RTP.
	MULAW_8K_AUDIO_CONFIG = internal_audio.NewMulaw8khzMonoAudioConfig()
)

// Streamer implements the TelephonyStreamer interface using native SIP signaling and RTP.
// No WebSocket needed — uses sipgo for signaling, RTP/UDP for audio.
//
// SIP manages its own context (derived from a parent session context) and
// concurrency primitives (RWMutex + atomic.Bool) because its lifecycle is
// tied to the SIP session rather than a simple background context.
type Streamer struct {
	internal_telephony_base.BaseTelephonyStreamer

	mu     sync.RWMutex
	closed atomic.Bool

	config     *sip_infra.Config
	session    *sip_infra.Session
	server     *sip_infra.Server
	rtpHandler *sip_infra.RTPHandler

	codec *sip_infra.Codec

	// SIP uses its own context derived from the session/parent context,
	// overriding the BaseStreamer context.
	ctx    context.Context
	cancel context.CancelFunc
}

// NewStreamer creates a SIP streamer.
//
// When sipSession is non-nil (inbound path), the streamer reuses the session's
// existing RTP handler from the global SIP server — no new server is created.
//
// When sipSession is nil (outbound / standalone path), a dedicated SIP server
// is spun up with its own RTP port pool and event handlers.
func NewStreamer(ctx context.Context,
	config *sip_infra.Config,
	logger commons.Logger,
	sipSession *sip_infra.Session,
	cc *callcontext.CallContext,
	vaultCred *protos.VaultCredential,
) (internal_type.Streamer, error) {
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

	// Bridge SIP streamer context to BaseStreamer context.
	// BaseStreamer.Ctx is created from context.Background() (by design), so
	// cancelling callCtx/streamerCtx does NOT make Recv() return. This
	// goroutine ensures that when the SIP session ends (BYE received,
	// callCtx cancelled), Recv() also returns io.EOF — preventing Talk()
	// from blocking indefinitely and leaking the RTP handler.
	go func() {
		<-streamerCtx.Done()
		s.BaseStreamer.Cancel()
	}()

	// --- Inbound: reuse existing session's RTP handler ---
	if sipSession != nil {
		// Check if parent context is already cancelled
		select {
		case <-ctx.Done():
			logger.Error("NewStreamer: Parent context already cancelled!", "err", ctx.Err())
		default:
			logger.Info("NewStreamer: Parent context is active")
		}

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

		logger.Info("NewStreamer: Starting forwardIncomingAudio goroutine")
		go s.forwardIncomingAudio()
		go s.runRTPWriter()
		s.PushInput(s.CreateConnectionRequest())

		localIP, localPort := rtpHandler.LocalAddr()
		logger.Infow("SIP streamer created (inbound)",
			"call_id", sipSession.GetCallID(),
			"codec", s.codec.Name,
			"rtp_port", localPort,
			"local_ip", localIP)

		return s, nil
	}

	// --- Outbound / standalone: spin up a dedicated SIP server ---
	// Address is the bind address (0.0.0.0 to listen on all interfaces).
	// config.Server is the remote SIP server — it must NOT be used as the
	// bind address or the RTP socket will fail to bind to a non-local IP.
	listenConfig := &sip_infra.ListenConfig{
		Address:   "0.0.0.0",
		Port:      config.Port,
		Transport: config.Transport,
	}

	tenantConfig := config
	configResolver := func(reqCtx *sip_infra.SIPRequestContext) (*sip_infra.InviteResult, error) {
		return &sip_infra.InviteResult{
			Config:      tenantConfig,
			ShouldAllow: true,
		}, nil
	}

	server, err := sip_infra.NewServer(streamerCtx, &sip_infra.ServerConfig{
		ListenConfig:      listenConfig,
		ConfigResolver:    configResolver,
		Logger:            logger,
		RTPPortRangeStart: config.RTPPortRangeStart,
		RTPPortRangeEnd:   config.RTPPortRangeEnd,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	s.server = server
	server.SetOnInvite(s.handleInvite)
	server.SetOnBye(s.handleBye)
	server.SetOnError(s.handleError)
	if err := server.Start(); err != nil {
		cancel()
		return nil, err
	}
	logger.Infow("SIP streamer created (outbound)")
	return s, nil
}

func (s *Streamer) handleInvite(session *sip_infra.Session, fromURI, toURI string) error {
	s.mu.Lock()
	s.session = session
	codec := s.codec
	s.mu.Unlock()

	// Allocate RTP port from the server's port pool
	rtpPort, err := s.server.AllocateRTPPort()
	if err != nil {
		return sip_infra.NewSIPError("handleInvite", session.GetCallID(), "no RTP ports available", err)
	}

	// Initialize RTP handler for audio
	rtpHandler, err := sip_infra.NewRTPHandler(s.ctx, &sip_infra.RTPConfig{
		LocalIP:     s.config.Server,
		LocalPort:   rtpPort,
		PayloadType: codec.PayloadType,
		ClockRate:   codec.ClockRate,
		Logger:      s.Logger,
	})
	if err != nil {
		s.server.ReleaseRTPPort(rtpPort)
		return sip_infra.NewSIPError("handleInvite", session.GetCallID(), "failed to create RTP handler", err)
	}

	s.mu.Lock()
	s.rtpHandler = rtpHandler
	s.mu.Unlock()

	// Update session with local RTP address
	localIP, localPort := rtpHandler.LocalAddr()
	session.SetLocalRTP(localIP, localPort)
	session.SetNegotiatedCodec(codec.Name, int(codec.ClockRate))
	session.SetRTPHandler(rtpHandler)

	// Start RTP processing
	rtpHandler.Start()

	// Start audio forwarding and RTP writer
	go s.forwardIncomingAudio()
	go s.runRTPWriter()

	s.Logger.Infow("SIP call established",
		"call_id", session.GetCallID(),
		"from", fromURI,
		"to", toURI,
		"codec", codec.Name)

	s.PushInputLow(&protos.ConversationEvent{
		Name: "channel",
		Data: map[string]string{
			"type":     "call_answered",
			"provider": "sip",
			"call_id":  session.GetCallID(),
			"codec":    codec.Name,
		},
		Time: timestamppb.Now(),
	})
	s.PushInput(s.CreateConnectionRequest())

	return nil
}

func (s *Streamer) handleBye(session *sip_infra.Session) error {
	s.Logger.Infow("BYE received, closing streamer", "call_id", session.GetCallID())
	s.PushInputLow(&protos.ConversationEvent{
		Name: "channel",
		Data: map[string]string{"type": "call_ended", "provider": "sip", "call_id": session.GetCallID()},
		Time: timestamppb.Now(),
	})
	return s.Close()
}

func (s *Streamer) handleError(session *sip_infra.Session, err error) {
	s.Logger.Error("SIP error occurred", "call_id", session.GetCallID(), "error", err)
	s.PushInputLow(&protos.ConversationEvent{
		Name: "channel",
		Data: map[string]string{"type": "error", "provider": "sip", "call_id": session.GetCallID(), "error_message": err.Error()},
		Time: timestamppb.Now(),
	})
}

// forwardIncomingAudio reads RTP audio packets, transcodes from A-law to
// µ-law when PCMA is negotiated, and pushes to InputCh via PushInput.
// Performing the A-law→µ-law conversion here (close to the source)
// keeps the channel-based pipeline simple and ensures the inputBuffer always
// contains µ-law data.
//
// Audio flow: RTP packets → [A-law→µ-law if PCMA] → inputBuffer → PushInput → InputCh
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

	// Get the current codec from the RTP handler (may have been updated by re-INVITE)
	codec := rtpHandler.GetCodec()

	// TTS produces LINEAR16 16kHz audio. Resample to µ-law 8kHz for RTP transmission.
	outData, err := s.Resampler().Resample(audioData, RAPIDA_AUDIO_CONFIG, MULAW_8K_AUDIO_CONFIG)
	if err != nil {
		s.Logger.Error("sendAudio: failed to resample audio", "error", err)
		return err
	}

	if codec != nil && codec.Name == "PCMA" {
		outData = mulawToAlaw(outData)
	}

	// Use BaseStreamer output buffer for consistent 20ms chunking.
	// BufferAndSendOutput accumulates audio and pushes 20ms frames to OutputCh.
	// runRTPWriter goroutine reads from OutputCh and forwards to RTP handler.
	s.BufferAndSendOutput(outData)
	return nil
}

// runRTPWriter reads pre-chunked 20ms frames from OutputCh and paces them
// to the RTP handler at 20ms intervals (matching real-time playback rate).
// This prevents overwhelming the RTP handler when TTS produces bursts of audio.
//
// The pacing pattern matches WebRTC's runOutputWriter:
// - Queue incoming frames in pendingAudio
// - Send one frame per 20ms tick
// - On FlushAudioCh, discard all queued audio
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
			// Interruption: discard all queued audio immediately.
			pendingAudio = pendingAudio[:0]
			// Also flush RTP handler's internal buffer.
			s.mu.RLock()
			rtpHandler := s.rtpHandler
			s.mu.RUnlock()
			if rtpHandler != nil {
				rtpHandler.FlushAudioOut()
			}

		case <-ticker.C:
			// Send one paced audio frame per tick (20ms real-time).
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
						// Channel full - shouldn't happen with pacing, but handle gracefully
						// s.Logger.Debug("runRTPWriter: RTP channel full, will retry next tick")
						continue // Don't consume from pendingAudio
					}
				}
				pendingAudio = pendingAudio[1:]
			}

		case msg := <-s.OutputCh:
			// Queue audio frame for paced sending.
			if m, ok := msg.(*protos.ConversationAssistantMessage); ok {
				if audio, ok := m.Message.(*protos.ConversationAssistantMessage_Audio); ok {
					pendingAudio = append(pendingAudio, audio.Audio)
				}
			}
		}
	}
}

func (s *Streamer) handleInterruption() error {
	// Clear BaseStreamer output buffer, which:
	// 1. Resets the output audio accumulation buffer
	// 2. Signals FlushAudioCh (runRTPWriter sees this and flushes RTP handler)
	// 3. Drains OutputCh (pending 20ms frames)
	s.ClearOutputBuffer()

	// s.Logger.Debug("Handled interruption, cleared output buffers")
	return nil
}

// Close closes the streamer and releases all resources.
// For SIP calls, this sends a BYE to the remote party (via session.Disconnect)
// before performing local cleanup, ensuring the remote PBX properly tears down
// the call leg.
func (s *Streamer) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}
	// Cancel context first
	s.cancel()
	s.BaseStreamer.Cancel() // ensure Recv() returns io.EOF

	s.mu.Lock()
	rtpHandler := s.rtpHandler
	server := s.server
	session := s.session
	s.rtpHandler = nil
	s.server = nil
	s.session = nil
	s.mu.Unlock()

	// Clear input buffer
	s.ResetInputBuffer()

	// Send SIP BYE to the remote party BEFORE local cleanup.
	// session.Disconnect() invokes the onDisconnect callback set by the SIP server
	// during INVITE handling, which calls Server.EndCall → sends BYE via the
	// dialog session. This ensures the remote PBX/provider sees a proper call
	// teardown instead of waiting for timeout.
	if session != nil {
		session.Disconnect()
	}

	// Stop RTP handler
	if rtpHandler != nil {
		if err := rtpHandler.Stop(); err != nil {
			s.Logger.Warn("Error stopping RTP handler", "error", err)
		}
	}

	// Stop server (only set for outbound/standalone streamers)
	if server != nil {
		server.Stop()
	}

	// End session (local cleanup — cancel context, close channels, set state)
	if session != nil {
		session.End()
	}

	s.Logger.Infow("SIP streamer closed")
	return nil
}

// mulawToAlaw converts μ-law (PCMU) to A-law (PCMA) for TTS output.
// Uses µ-law → PCM16 → A-law path because g711.Ulaw2Alaw() has a bug.
func mulawToAlaw(in []byte) []byte {
	return g711.EncodeAlaw(g711.DecodeUlaw(in))
}
