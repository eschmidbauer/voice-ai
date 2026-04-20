// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_webrtc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	pionwebrtc "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	assistant_config "github.com/rapidaai/api/assistant-api/config"
	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	internal_audio_resampler "github.com/rapidaai/api/assistant-api/internal/audio/resampler"
	channel_base "github.com/rapidaai/api/assistant-api/internal/channel/base"
	webrtc_internal "github.com/rapidaai/api/assistant-api/internal/channel/webrtc/internal"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// webrtcStreamer implements Streamer using Pion WebRTC for media and gRPC for signaling.
type webrtcStreamer struct {
	channel_base.BaseStreamer // channels, buffers, Input/Output, Recv, Context

	// WebRTC-specific components
	config     *webrtc_internal.Config
	webrtcCfg  *assistant_config.WebRTCConfig // nil = local/default behaviour
	grpcStream grpc.BidiStreamingServer[protos.WebTalkRequest, protos.WebTalkResponse]

	// Session state
	sessionID string

	// Pion WebRTC
	pc         *pionwebrtc.PeerConnection
	localTrack *pionwebrtc.TrackLocalStaticSample
	resampler  internal_type.AudioResampler
	opusCodec  *webrtc_internal.OpusCodec

	// Audio processing context - cancelled on audio disconnect/reconnect
	audioCtx    context.Context
	audioCancel context.CancelFunc
	audioWg     sync.WaitGroup // Tracks audio goroutines for clean shutdown

	currentMode protos.StreamMode

	// closed guards Close() for idempotency across concurrent goroutines.
	closed atomic.Bool

	// peerConnected is set to true when the WebRTC peer connection reaches
	// Connected state. runOutputWriter gates audio writes on this flag
	// to prevent WriteSample from silently dropping frames before the
	// SRTP session is established. Uses atomic for lock-free access from
	// runOutputWriter's hot loop.
	peerConnected atomic.Bool

	// iceStartedAt records when ICE negotiation began (inside setupAudioAndHandshake)
	// so we can report ICE latency in the peer_connected observability event.
	iceStartedAt time.Time
}

// NewWebRTCStreamer creates a new WebRTC streamer with gRPC signaling.
// The streamer owns its own context (derived from context.Background) so that
// cleanup is never short-circuited by the caller's context being cancelled first.
// A separate goroutine watches the caller's context and triggers a graceful close.
func NewWebRTCStreamer(
	ctx context.Context,
	logger commons.Logger,
	grpcStream grpc.BidiStreamingServer[protos.WebTalkRequest, protos.WebTalkResponse],
	webrtcCfg *assistant_config.WebRTCConfig,
) (internal_type.Streamer, error) {
	resampler, err := internal_audio_resampler.GetResampler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create resampler: %w", err)
	}

	opusCodec, err := webrtc_internal.NewOpusCodec()
	if err != nil {
		return nil, fmt.Errorf("failed to create Opus codec: %w", err)
	}

	s := &webrtcStreamer{
		BaseStreamer: channel_base.NewBaseStreamer(logger,
			channel_base.WithInputChannelSize(webrtc_internal.InputChannelSize),
			channel_base.WithOutputChannelSize(webrtc_internal.OutputChannelSize),
			channel_base.WithInputBufferThreshold(webrtc_internal.InputBufferThreshold),
			channel_base.WithOutputBufferThreshold(webrtc_internal.OutputBufferThreshold),
			channel_base.WithOutputFrameSize(webrtc_internal.OpusFrameBytes),
		),
		config:      webrtc_internal.DefaultConfig(),
		webrtcCfg:   webrtcCfg,
		grpcStream:  grpcStream,
		sessionID:   uuid.New().String(),
		resampler:   resampler,
		opusCodec:   opusCodec,
		currentMode: protos.StreamMode_STREAM_MODE_TEXT,
		// peerConnected zero-value is false — correct: not connected yet
	}

	// Start background loops
	go s.runGrpcReader()   // inputCh feeder
	go s.runOutputWriter() // outputCh consumer

	// Watch the caller's context so a cancelled parent triggers graceful close
	// rather than an abrupt context cancellation mid-cleanup.
	go s.watchCallerContext(ctx)

	return s, nil
}

func (s *webrtcStreamer) stopAudioProcessing() {
	s.Mu.Lock()
	if s.audioCancel != nil {
		s.audioCancel()
		s.audioCancel = nil
	}
	s.audioCtx = nil
	s.Mu.Unlock()
	s.audioWg.Wait()
}

func (s *webrtcStreamer) createPeerConnection() error {
	// Create new audio context and fresh output channel for this connection
	s.Mu.Lock()
	s.audioCtx, s.audioCancel = context.WithCancel(s.Ctx)
	s.Mu.Unlock()
	s.peerConnected.Store(false) // reset for new connection cycle

	mediaEngine := &pionwebrtc.MediaEngine{}
	if err := mediaEngine.RegisterCodec(pionwebrtc.RTPCodecParameters{
		RTPCodecCapability: pionwebrtc.RTPCodecCapability{
			MimeType:    pionwebrtc.MimeTypeOpus,
			ClockRate:   webrtc_internal.OpusSampleRate,
			Channels:    webrtc_internal.OpusChannels,
			SDPFmtpLine: webrtc_internal.OpusSDPFmtpLine,
		},
		PayloadType: webrtc_internal.OpusPayloadType,
	}, pionwebrtc.RTPCodecTypeAudio); err != nil {
		return fmt.Errorf("failed to register Opus codec: %w", err)
	}

	// Interceptors (default includes NACK for audio packet recovery)
	registry := &interceptor.Registry{}
	if err := pionwebrtc.RegisterDefaultInterceptors(mediaEngine, registry); err != nil {
		return fmt.Errorf("failed to register interceptors: %w", err)
	}

	settingEngine := pionwebrtc.SettingEngine{}
	if s.webrtcCfg != nil {
		if s.webrtcCfg.ExternalIP != "" {
			if err := settingEngine.SetICEAddressRewriteRules(pionwebrtc.ICEAddressRewriteRule{
				External:        []string{s.webrtcCfg.ExternalIP},
				AsCandidateType: pionwebrtc.ICECandidateTypeHost,
			}); err != nil {
				return fmt.Errorf("failed to set ICE address rewrite rules: %w", err)
			}
		}
		if s.webrtcCfg.UDPPortRangeStart > 0 && s.webrtcCfg.UDPPortRangeEnd > 0 {
			if err := settingEngine.SetEphemeralUDPPortRange(
				uint16(s.webrtcCfg.UDPPortRangeStart),
				uint16(s.webrtcCfg.UDPPortRangeEnd),
			); err != nil {
				return fmt.Errorf("failed to set UDP port range: %w", err)
			}
		}
	}

	api := pionwebrtc.NewAPI(
		pionwebrtc.WithMediaEngine(mediaEngine),
		pionwebrtc.WithInterceptorRegistry(registry),
		pionwebrtc.WithSettingEngine(settingEngine),
	)

	iceServers := make([]pionwebrtc.ICEServer, len(s.config.ICEServers))
	for i, srv := range s.config.ICEServers {
		iceServers[i] = pionwebrtc.ICEServer{
			URLs:       srv.URLs,
			Username:   srv.Username,
			Credential: srv.Credential,
		}
	}

	pcConfig := pionwebrtc.Configuration{ICEServers: iceServers}
	if s.config.ICETransportPolicy == "relay" {
		pcConfig.ICETransportPolicy = pionwebrtc.ICETransportPolicyRelay
	}

	pc, err := api.NewPeerConnection(pcConfig)
	if err != nil {
		return fmt.Errorf("failed to create peer connection: %w", err)
	}

	s.Mu.Lock()
	s.pc = pc
	s.Mu.Unlock()

	s.setupPeerEventHandlers()
	return s.createLocalTrack()
}

func (s *webrtcStreamer) setupPeerEventHandlers() {
	// ICE candidates are NOT sent individually (no trickle ICE).
	// Instead we wait for gathering to complete in initiateWebRTCHandshake()
	// and embed all candidates inline in the offer SDP. Safari has consistent
	// issues processing trickle ICE candidates that arrive as separate messages
	// after the offer/answer exchange; a complete offer avoids those bugs.
	s.pc.OnICECandidate(func(c *pionwebrtc.ICECandidate) {
		// no-op: candidates are captured via LocalDescription() after gathering
	})

	// Connection state
	s.pc.OnConnectionStateChange(func(state pionwebrtc.PeerConnectionState) {
		s.Logger.Infow("WebRTC connection state changed", "state", state, "session", s.sessionID)

		// Update mode under lock, then release before any channel operations
		// to avoid holding mu while pushing to outputCh.
		s.Mu.Lock()
		switch state {
		case pionwebrtc.PeerConnectionStateConnected:
			s.currentMode = protos.StreamMode_STREAM_MODE_AUDIO
		case pionwebrtc.PeerConnectionStateFailed,
			pionwebrtc.PeerConnectionStateClosed,
			pionwebrtc.PeerConnectionStateDisconnected:
			s.currentMode = protos.StreamMode_STREAM_MODE_TEXT
		}
		s.Mu.Unlock()

		// Perform channel / lifecycle operations outside the lock.
		switch state {
		case pionwebrtc.PeerConnectionStateConnected:
			// Unblock runOutputWriter so buffered greeting audio can drain.
			s.peerConnected.Store(true)
			s.Mu.Lock()
			iceLatencyMs := time.Since(s.iceStartedAt).Milliseconds()
			s.Mu.Unlock()
			s.Input(&protos.ConversationEvent{
				Name: "webrtc",
				Data: map[string]string{
					"type":           "peer_connected",
					"session_id":     s.sessionID,
					"ice_latency_ms": fmt.Sprintf("%d", iceLatencyMs),
				},
				Time: timestamppb.Now(),
			})
			s.sendReady()

		case pionwebrtc.PeerConnectionStateFailed:
			// ICE failure — common on Safari (mDNS privacy candidates cannot be
			// resolved on a cloud server). Do NOT close the gRPC session; fall
			// back to text mode so the conversation stays alive.
			s.Logger.Warnw("WebRTC ICE failed, falling back to text mode", "session", s.sessionID)
			s.Input(&protos.ConversationEvent{
				Name: "webrtc",
				Data: map[string]string{
					"type":       "peer_failed",
					"session_id": s.sessionID,
					"reason":     "ice_failed",
				},
				Time: timestamppb.Now(),
			})
			s.resetAudioSession()

		case pionwebrtc.PeerConnectionStateDisconnected:
			// Transient state — network hiccup, ICE may recover.
			// Only reset audio; do NOT close the gRPC stream/context so the
			// session can continue in text mode or reconnect.
			s.Logger.Warnw("WebRTC peer disconnected, resetting audio", "session", s.sessionID)
			s.Input(&protos.ConversationEvent{
				Name: "webrtc",
				Data: map[string]string{
					"type":       "peer_disconnected",
					"session_id": s.sessionID,
				},
				Time: timestamppb.Now(),
			})
			s.resetAudioSession()
		}
	})

	// Remote track (incoming audio)
	s.pc.OnTrack(func(track *pionwebrtc.TrackRemote, _ *pionwebrtc.RTPReceiver) {
		if track.Kind() != pionwebrtc.RTPCodecTypeAudio {
			return
		}
		s.Logger.Infow("Remote audio track received", "codec", track.Codec().MimeType)
		s.Input(&protos.ConversationEvent{
			Name: "webrtc",
			Data: map[string]string{
				"type":       "audio_track_received",
				"session_id": s.sessionID,
				"codec":      track.Codec().MimeType,
			},
			Time: timestamppb.Now(),
		})
		// Add to WaitGroup before launching goroutine to prevent
		// audioWg.Wait() from racing with audioWg.Add(1).
		s.audioWg.Add(1)
		go s.readRemoteAudio(track)
	})
}

func (s *webrtcStreamer) createLocalTrack() error {
	track, err := pionwebrtc.NewTrackLocalStaticSample(
		pionwebrtc.RTPCodecCapability{
			MimeType:  pionwebrtc.MimeTypeOpus,
			ClockRate: webrtc_internal.OpusSampleRate,
			Channels:  webrtc_internal.OpusChannels,
		},
		"audio",
		"rapida-audio",
	)
	if err != nil {
		return fmt.Errorf("failed to create local audio track: %w", err)
	}

	if _, err := s.pc.AddTrack(track); err != nil {
		return fmt.Errorf("failed to add track: %w", err)
	}

	s.Mu.Lock()
	s.localTrack = track
	s.Mu.Unlock()
	return nil
}

// readRemoteAudio decodes Opus → PCM, resamples 48kHz → 16kHz, pushes to inputCh.
func (s *webrtcStreamer) readRemoteAudio(track *pionwebrtc.TrackRemote) {
	defer s.audioWg.Done()

	s.Mu.Lock()
	audioCtx := s.audioCtx
	s.Mu.Unlock()

	if audioCtx == nil {
		return
	}

	mimeType := track.Codec().MimeType
	if mimeType != pionwebrtc.MimeTypeOpus {
		s.Logger.Errorw("Unsupported codec, only Opus is supported", "codec", mimeType)
		return
	}

	opusDecoder, err := webrtc_internal.NewOpusCodec()
	if err != nil {
		s.Logger.Errorw("Failed to create Opus decoder", "error", err)
		return
	}

	buf := make([]byte, webrtc_internal.RTPBufferSize)
	consecutiveErrors := 0

	for {
		select {
		case <-audioCtx.Done():
			return
		default:
		}

		n, _, err := track.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= webrtc_internal.MaxConsecutiveErrors {
				s.Logger.Errorw("Too many consecutive read errors, stopping audio reader", "lastError", err)
				return
			}
			continue
		}
		consecutiveErrors = 0

		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			s.Logger.Debugw("Failed to unmarshal RTP packet", "error", err)
			continue
		}
		if len(pkt.Payload) == 0 {
			continue
		}

		pcm, err := opusDecoder.Decode(pkt.Payload)
		if err != nil {
			s.Logger.Debugw("Opus decode failed", "error", err, "payloadSize", len(pkt.Payload))
			continue
		}
		resampled, err := s.resampler.Resample(pcm, internal_audio.WEBRTC_AUDIO_CONFIG, internal_audio.RAPIDA_INTERNAL_AUDIO_CONFIG)
		if err != nil {
			s.Logger.Debugw("Audio resample failed", "error", err)
			continue
		}

		s.BufferAndSendInput(resampled)
	}
}

// runOutputWriter drains outputCh: audio → Opus-encode → WebRTC track (paced 20ms);
// non-audio → wrap in WebTalkResponse → gRPC.
func (s *webrtcStreamer) runOutputWriter() {
	ticker := time.NewTicker(time.Duration(webrtc_internal.OutputPaceInterval) * time.Millisecond)
	defer ticker.Stop()

	var pendingAudio [][]byte

	for {
		select {
		case <-s.Ctx.Done():
			return

		case <-s.FlushAudioCh:
			pendingAudio = pendingAudio[:0]

		case <-ticker.C:
			if len(pendingAudio) > 0 && s.peerConnected.Load() {
				encoded, err := s.opusCodec.Encode(pendingAudio[0])
				if err != nil {
					s.Logger.Debugw("Opus encode failed", "error", err)
				} else {
					s.writeAudioFrame(encoded)
				}
				pendingAudio = pendingAudio[1:]
			}

		case msg := <-s.OutputCh:
			if m, ok := msg.(*protos.ConversationAssistantMessage); ok {
				if audio, ok := m.Message.(*protos.ConversationAssistantMessage_Audio); ok {
					pendingAudio = append(pendingAudio, audio.Audio)
					continue
				}
			}

			if resp := s.buildGRPCResponse(msg); resp != nil {
				s.dispatchOutput(resp)
			}
		}
	}
}

// buildGRPCResponse wraps a raw proto type into a WebTalkResponse for gRPC.
// Pre-built *WebTalkResponse (e.g. signaling) are passed through as-is.
func (s *webrtcStreamer) buildGRPCResponse(msg internal_type.Stream) *protos.WebTalkResponse {
	resp := &protos.WebTalkResponse{Code: 200, Success: true}
	switch m := msg.(type) {
	case *protos.ConversationAssistantMessage:
		resp.Data = &protos.WebTalkResponse_Assistant{Assistant: m}
	case *protos.ConversationConfiguration:
		resp.Data = &protos.WebTalkResponse_Configuration{Configuration: m}
	case *protos.ConversationInitialization:
		resp.Data = &protos.WebTalkResponse_Initialization{Initialization: m}
	case *protos.ConversationUserMessage:
		resp.Data = &protos.WebTalkResponse_User{User: m}
	case *protos.ConversationInterruption:
		resp.Data = &protos.WebTalkResponse_Interruption{Interruption: m}
	case *protos.ConversationToolCall:
		resp.Data = &protos.WebTalkResponse_ToolCall{ToolCall: m}
	case *protos.ConversationError:
		resp.Data = &protos.WebTalkResponse_Error{Error: m}
	case *protos.ConversationEvent:
		resp.Data = &protos.WebTalkResponse_Event{Event: m}
	case *protos.ConversationMetadata:
		resp.Data = &protos.WebTalkResponse_Metadata{Metadata: m}
	case *protos.ConversationMetric:
		resp.Data = &protos.WebTalkResponse_Metric{Metric: m}
	case *protos.ServerSignaling:
		resp.Data = &protos.WebTalkResponse_Signaling{Signaling: m}
	default:
		s.Logger.Warnw("Unknown output message type, skipping", "type", fmt.Sprintf("%T", msg))
		return nil
	}
	return resp
}

// dispatchOutput sends a WebTalkResponse directly to the gRPC stream.
func (s *webrtcStreamer) dispatchOutput(resp *protos.WebTalkResponse) {
	if err := s.grpcStream.Send(resp); err != nil {
		s.Logger.Errorw("Failed to send gRPC response", "error", err)
	}
}

// writeAudioFrame writes an encoded Opus frame to the WebRTC local track.
func (s *webrtcStreamer) writeAudioFrame(data []byte) {
	s.Mu.Lock()
	track := s.localTrack
	s.Mu.Unlock()

	if track == nil {
		return
	}
	if err := track.WriteSample(media.Sample{
		Data:     data,
		Duration: webrtc_internal.OpusFrameDuration * time.Millisecond,
	}); err != nil {
		s.Logger.Debugw("Failed to write sample to track", "error", err)
	}
}

func (s *webrtcStreamer) sendConfig() {
	iceServers := make([]*protos.ICEServer, len(s.config.ICEServers))
	for i, srv := range s.config.ICEServers {
		iceServers[i] = &protos.ICEServer{
			Urls:       srv.URLs,
			Username:   srv.Username,
			Credential: srv.Credential,
		}
	}

	s.Output(&protos.ServerSignaling{
		SessionId: s.sessionID,
		Message: &protos.ServerSignaling_Config{
			Config: &protos.WebRTCConfig{
				IceServers: iceServers,
				AudioCodec: "opus",
				SampleRate: int32(webrtc_internal.OpusSampleRate),
			},
		},
	},
	)
}

func (s *webrtcStreamer) sendOffer(sdp string) {
	s.Output(&protos.ServerSignaling{
		SessionId: s.sessionID,
		Message: &protos.ServerSignaling_Sdp{
			Sdp: &protos.WebRTCSDP{
				Type: protos.WebRTCSDP_OFFER,
				Sdp:  sdp,
			},
		},
	})
}

func (s *webrtcStreamer) sendICECandidate(ice *webrtc_internal.ICECandidate) {
	s.Output(&protos.ServerSignaling{
		SessionId: s.sessionID,
		Message: &protos.ServerSignaling_IceCandidate{
			IceCandidate: &protos.ICECandidate{
				Candidate:        ice.Candidate,
				SdpMid:           ice.SDPMid,
				SdpMLineIndex:    int32(ice.SDPMLineIndex),
				UsernameFragment: ice.UsernameFragment,
			},
		},
	})
}

func (s *webrtcStreamer) sendReady() {
	s.Output(&protos.ServerSignaling{
		SessionId: s.sessionID,
		Message:   &protos.ServerSignaling_Ready{Ready: true},
	})
}

func (s *webrtcStreamer) sendClear() {
	s.Output(&protos.ServerSignaling{
		SessionId: s.sessionID,
		Message:   &protos.ServerSignaling_Clear{Clear: true},
	})
}

// runGrpcReader reads from the gRPC stream in a loop and pushes
// non-signaling messages into inputCh. Signaling is handled internally.
// Runs until the gRPC stream closes or the context is cancelled.
func (s *webrtcStreamer) runGrpcReader() {
	for {
		msg, err := s.grpcStream.Recv()
		if err != nil {
			if disc := s.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); disc != nil {
				s.Input(disc)
			}
			return
		}
		switch msg.GetRequest().(type) {
		case *protos.WebTalkRequest_Initialization:
			s.Input(msg.GetInitialization())
		case *protos.WebTalkRequest_Configuration:
			s.Input(msg.GetConfiguration())
			s.handleConfigurationMessage(msg.GetConfiguration().GetStreamMode())
		case *protos.WebTalkRequest_Message:
			s.Input(msg.GetMessage())
		case *protos.WebTalkRequest_Metadata:
			s.Input(msg.GetMetadata())
		case *protos.WebTalkRequest_Metric:
			s.Input(msg.GetMetric())
		case *protos.WebTalkRequest_ToolCallResult:
			s.Input(msg.GetToolCallResult())
		case *protos.WebTalkRequest_Signaling:
			s.handleClientSignaling(msg.GetSignaling())
		default:
			s.Logger.Warnw("Unknown message type", "type", fmt.Sprintf("%T", msg.GetRequest()))
		}
	}
}

// NotifyMode is called by the Talk loop after Connect() completes.
// For AUDIO mode it triggers the WebRTC handshake; for TEXT it is a no-op
// (or tears down audio if switching back to text).
func (s *webrtcStreamer) NotifyMode(mode protos.StreamMode) {
	s.handleConfigurationMessage(mode)
}

// handleConfigurationMessage processes transport mode changes.
// Switching text <-> audio only changes I/O transport - it does NOT create a new session.
func (s *webrtcStreamer) handleConfigurationMessage(mode protos.StreamMode) {
	s.Mu.Lock()
	currentMode := s.currentMode
	s.Mu.Unlock()

	if mode == currentMode {
		return
	}

	switch mode {
	case protos.StreamMode_STREAM_MODE_AUDIO:
		if err := s.setupAudioAndHandshake(); err != nil {
			s.Logger.Errorw("Failed to setup audio", "error", err)
			s.resetAudioSession()
		}
	case protos.StreamMode_STREAM_MODE_TEXT:
		s.resetAudioSession()
	}
}

// handleClientSignaling processes client WebRTC signaling messages
func (s *webrtcStreamer) handleClientSignaling(signaling *protos.ClientSignaling) {
	s.Mu.Lock()
	pc := s.pc
	s.Mu.Unlock()

	switch msg := signaling.GetMessage().(type) {
	case *protos.ClientSignaling_Sdp:
		if msg.Sdp.GetType() == protos.WebRTCSDP_ANSWER {
			if pc == nil {
				s.Logger.Warnw("Received SDP answer but peer connection is nil, ignoring")
				return
			}
			if err := pc.SetRemoteDescription(pionwebrtc.SessionDescription{
				Type: pionwebrtc.SDPTypeAnswer,
				SDP:  msg.Sdp.GetSdp(),
			}); err != nil {
				s.Logger.Errorw("Failed to set remote description", "error", err)
			}
		}

	case *protos.ClientSignaling_IceCandidate:
		if pc == nil {
			s.Logger.Warnw("Received ICE candidate but peer connection is nil, ignoring")
			return
		}
		ice := msg.IceCandidate
		idx := uint16(ice.GetSdpMLineIndex())
		sdpMid := ice.GetSdpMid()
		usernameFragment := ice.GetUsernameFragment()
		if err := pc.AddICECandidate(pionwebrtc.ICECandidateInit{
			Candidate:        ice.GetCandidate(),
			SDPMid:           &sdpMid,
			SDPMLineIndex:    &idx,
			UsernameFragment: &usernameFragment,
		}); err != nil {
			// Non-fatal: with complete ICE we use candidates embedded in the SDP;
			// individual trickle candidates are unnecessary and may arrive before
			// remote description is set on older clients. Log at Warn, not Error.
			s.Logger.Warnw("Failed to add ICE candidate (non-fatal)", "error", err)
		}

	case *protos.ClientSignaling_Disconnect:
		if msg.Disconnect {
			if disc := s.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); disc != nil {
				s.Input(disc)
			}
		}
	}
}

func (s *webrtcStreamer) resetAudioSession() {
	s.stopAudioProcessing()
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.pc != nil {
		s.pc.Close()
		s.pc = nil
	}
	s.localTrack = nil
	s.currentMode = protos.StreamMode_STREAM_MODE_TEXT
	s.peerConnected.Store(false)
}

// setupAudioAndHandshake tears down any stale peer connection, creates a fresh
// one, and initiates the WebRTC handshake (config -> offer -> answer -> ICE).
func (s *webrtcStreamer) setupAudioAndHandshake() error {
	// Always start fresh
	s.Mu.Lock()
	if s.pc != nil {
		s.pc.Close()
		s.pc = nil
		s.localTrack = nil
	}
	s.iceStartedAt = time.Now()
	s.Mu.Unlock()

	if err := s.createPeerConnection(); err != nil {
		return fmt.Errorf("failed to create peer connection: %w", err)
	}

	return s.initiateWebRTCHandshake()
}

// initiateWebRTCHandshake sends config and a complete SDP offer.
//
// We wait for ICE gathering to finish before sending the offer so all
// candidates are embedded inline in the SDP (complete ICE, not trickle).
// Safari consistently fails when ICE candidates arrive as separate messages
// after the offer; embedding them in the SDP avoids those timing bugs.
func (s *webrtcStreamer) initiateWebRTCHandshake() error {
	s.sendConfig()

	if _, err := s.createAndSetLocalOffer(); err != nil {
		return err
	}

	// Wait for ICE gathering so LocalDescription has all candidates.
	gatherComplete := pionwebrtc.GatheringCompletePromise(s.pc)

	s.Mu.Lock()
	audioCtx := s.audioCtx
	s.Mu.Unlock()

	select {
	case <-gatherComplete:
		// all candidates gathered
	case <-time.After(5 * time.Second):
		s.Logger.Warnw("ICE gathering timed out after 5s, sending partial offer", "session", s.sessionID)
	case <-audioCtx.Done():
		return fmt.Errorf("context cancelled during ICE gathering")
	}

	finalDesc := s.pc.LocalDescription()
	if finalDesc == nil {
		return fmt.Errorf("local description is nil after ICE gathering")
	}
	s.sendOffer(finalDesc.SDP)
	return nil
}

// createAndSetLocalOffer creates SDP offer and sets it as local description.
func (s *webrtcStreamer) createAndSetLocalOffer() (*pionwebrtc.SessionDescription, error) {
	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create offer: %w", err)
	}

	if err := s.pc.SetLocalDescription(offer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	return &offer, nil
}

func (s *webrtcStreamer) Send(response internal_type.Stream) error {
	switch data := response.(type) {
	case *protos.ConversationAssistantMessage:
		switch content := data.Message.(type) {
		case *protos.ConversationAssistantMessage_Audio:
			audio48kHz, err := s.resampler.Resample(content.Audio, internal_audio.RAPIDA_INTERNAL_AUDIO_CONFIG, internal_audio.WEBRTC_AUDIO_CONFIG)
			if err != nil {
				return err
			}
			s.BufferAndSendOutput(audio48kHz)
			return nil
		case *protos.ConversationAssistantMessage_Text:
			s.Output(data)
		}
	case *protos.ConversationConfiguration:
		s.Output(data)
	case *protos.ConversationInitialization:
		s.Output(data)
	case *protos.ConversationUserMessage:
		s.Output(data)
	case *protos.ConversationInterruption:
		if data.Type == protos.ConversationInterruption_INTERRUPTION_TYPE_WORD {
			s.ClearOutputBuffer()
			s.sendClear()
		}
		s.Output(data)
	case *protos.ConversationToolCall:
		s.Output(data)
		if data.GetAction() == protos.ToolCallAction_TOOL_CALL_ACTION_END_CONVERSATION {
			s.Input(&protos.ConversationToolCallResult{
				Id:     data.GetId(),
				ToolId: data.GetToolId(),
				Name:   data.GetName(),
				Action: data.GetAction(),
				Result: map[string]string{"status": "completed"},
			})
			if disc := s.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_TOOL); disc != nil {
				s.Input(disc)
			}
		}
	case *protos.ConversationError:
		s.Output(data)
	case *protos.ConversationEvent:
		s.Output(data)
	case *protos.ConversationMetadata:
		s.Output(data)
	case *protos.ConversationDisconnection:
		s.Output(data)
	case *protos.ConversationMetric:
		s.Output(data)
	}
	return nil
}

func (s *webrtcStreamer) watchCallerContext(callerCtx context.Context) {
	select {
	case <-callerCtx.Done():
		s.Logger.Infow("Caller context cancelled, closing streamer gracefully", "session", s.sessionID)
		s.Close()
	case <-s.Ctx.Done():
		// Streamer already closed on its own, nothing to do.
	}
}

// Close is idempotent — safe to call from multiple goroutines.
func (s *webrtcStreamer) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if disc := s.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER); disc != nil {
		s.Input(disc)
	}
	s.stopAudioProcessing()

	s.Mu.Lock()
	if s.pc != nil {
		s.pc.Close()
		s.pc = nil
	}
	s.localTrack = nil
	s.Mu.Unlock()

	s.Cancel()
	return nil
}
