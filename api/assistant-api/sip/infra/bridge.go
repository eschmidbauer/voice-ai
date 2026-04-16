// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_infra

import (
	"context"
	"fmt"
	"time"

	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
)

// MakeBridgeCall initiates an outbound SIP call synchronously and returns the
// answered session with active RTP. Unlike MakeCall, it does NOT trigger the
// pipeline (no onInvite callback, no Talk loop). The caller owns the returned
// session lifecycle and must call session.End() when done.
//
// Used for B2BUA bridge transfers: the platform places a new INVITE to the
// transfer target, bridges RTP between the inbound and outbound sessions,
// and the AI drops out of the audio path.
func (s *Server) MakeBridgeCall(ctx context.Context, cfg *Config, toURI, fromURI string) (*Session, error) {
	if s.state.Load() != int32(ServerStateRunning) {
		return nil, fmt.Errorf("SIP server is not running")
	}

	// Prepare outbound INVITE (shared with MakeCall)
	invite, err := s.prepareOutboundInvite(ctx, cfg, toURI, fromURI)
	if err != nil {
		return nil, err
	}

	s.logger.Infow("MakeBridgeCall sending INVITE",
		"to", toURI, "from", fromURI, "call_id", invite.callID)

	// Block until 200 OK or failure (MakeCall does this in handleOutboundDialog goroutine)
	answered, err := s.waitForAnswer(ctx, invite, cfg)
	if err != nil {
		invite.cleanup()
		return nil, NewSIPError("MakeBridgeCall", invite.callID, "call not answered", err)
	}

	// Create session (no auth/assistant/vault — bridge leg is infrastructure-only)
	session, err := NewSession(context.Background(), &SessionConfig{
		Config:    cfg,
		Direction: CallDirectionOutbound,
		CallID:    invite.callID,
		Codec:     &CodecPCMU,
		Logger:    s.logger,
	})
	if err != nil {
		answered.rtpHandler.Stop()
		s.rtpAllocator.Release(invite.rtpPort)
		// Dialog is already confirmed (ACK sent) — send BYE before closing
		byeCtx, byeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer byeCancel()
		if byeErr := invite.dialogSession.Bye(byeCtx); byeErr != nil {
			s.logger.Warnw("MakeBridgeCall: failed to send BYE on setup failure",
				"call_id", invite.callID, "error", byeErr)
		}
		invite.dialogSession.Close()
		return nil, NewSIPError("MakeBridgeCall", invite.callID, "session creation failed", err)
	}

	session.SetLocalRTP(invite.externalIP, invite.localPort)
	session.SetRTPHandler(answered.rtpHandler)
	session.SetDialogClientSession(invite.dialogSession)
	session.SetState(CallStateConnected)

	if answered.negotiatedCodec != nil {
		session.SetNegotiatedCodec(answered.negotiatedCodec.Name, int(answered.negotiatedCodec.ClockRate))
	}

	// Register for BYE routing
	s.registerSession(session, invite.callID)

	s.logger.Infow("Bridge call answered", "call_id", invite.callID, "to", toURI)
	return session, nil
}

// BridgeTransfer bridges RTP audio between two sessions. Blocks until one side
// hangs up, a safety timeout is reached, or the context is cancelled.
// Tears down both sessions on exit.
func (s *Server) BridgeTransfer(ctx context.Context, inbound, outbound *Session) error {
	inCallID := inbound.GetCallID()
	outCallID := outbound.GetCallID()

	inRTP := inbound.GetRTPHandler()
	outRTP := outbound.GetRTPHandler()
	if inRTP == nil || outRTP == nil {
		if !outbound.IsEnded() {
			outbound.End()
		}
		if !inbound.IsEnded() {
			inbound.End()
		}
		return NewSIPError("BridgeTransfer", inCallID, "RTP handler unavailable", ErrRTPNotInitialized)
	}

	inCodec := inbound.GetNegotiatedCodec()
	outCodec := outbound.GetNegotiatedCodec()
	needsTranscode := inCodec != nil && outCodec != nil && inCodec.Name != outCodec.Name

	s.logger.Infow("Audio bridge started",
		"inbound_call_id", inCallID,
		"outbound_call_id", outCallID,
		"inbound_codec", s.codecName(inCodec),
		"outbound_codec", s.codecName(outCodec),
		"transcoding", needsTranscode)

	audioCtx, audioCancel := context.WithCancel(ctx)
	defer audioCancel()

	// Cross-pipe RTP audio between the two sessions
	go s.forwardBridgeAudio(audioCtx, inRTP.AudioIn(), outRTP.AudioOut(), needsTranscode, inCodec, outCodec)
	go s.forwardBridgeAudio(audioCtx, outRTP.AudioIn(), inRTP.AudioOut(), needsTranscode, outCodec, inCodec)

	// Wait for either side to hang up
	select {
	case <-ctx.Done():
		s.logger.Infow("Bridge: context cancelled",
			"inbound_call_id", inCallID, "outbound_call_id", outCallID, "error", ctx.Err())
	case <-inbound.ByeReceived():
		s.logger.Infow("Bridge: inbound caller hung up", "inbound_call_id", inCallID)
	case <-outbound.ByeReceived():
		s.logger.Infow("Bridge: transfer target hung up", "outbound_call_id", outCallID)
	case <-inbound.Context().Done():
		s.logger.Infow("Bridge: inbound session ended", "inbound_call_id", inCallID)
	case <-outbound.Context().Done():
		s.logger.Infow("Bridge: outbound session ended", "outbound_call_id", outCallID)
	case <-time.After(BridgeSafetyTimeout):
		s.logger.Warnw("Bridge: safety timeout reached, tearing down",
			"inbound_call_id", inCallID, "outbound_call_id", outCallID)
	}

	audioCancel()
	if !outbound.IsEnded() {
		outbound.End()
	}
	if !inbound.IsEnded() {
		inbound.End()
	}

	s.logger.Infow("Audio bridge completed",
		"inbound_call_id", inCallID, "outbound_call_id", outCallID)
	return nil
}

// forwardBridgeAudio reads audio from src and writes to dst, transcoding if needed.
func (s *Server) forwardBridgeAudio(ctx context.Context, src <-chan []byte, dst chan<- []byte, needsTranscode bool, srcCodec, dstCodec *Codec) {
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-src:
			if !ok {
				return
			}
			if needsTranscode {
				data = s.transcodeG711(data, srcCodec, dstCodec)
			}
			select {
			case dst <- data:
			case <-ctx.Done():
				return
			default:
				// dst full — drop frame to prevent stall
			}
		}
	}
}

// transcodeG711 converts audio between PCMU and PCMA codecs.
func (s *Server) transcodeG711(data []byte, from, to *Codec) []byte {
	if from.Name == "PCMA" && to.Name == "PCMU" {
		return internal_audio.AlawToUlaw(data)
	}
	if from.Name == "PCMU" && to.Name == "PCMA" {
		return internal_audio.UlawToAlaw(data)
	}
	return data
}

func (s *Server) codecName(c *Codec) string {
	if c != nil {
		return c.Name
	}
	return "PCMU"
}
