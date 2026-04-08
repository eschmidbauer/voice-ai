// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_infra

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/google/uuid"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
)

// Session channel buffer sizes
const (
	eventBufferSize = 50
	errorBufferSize = 10
)

// SessionConfig holds configuration for creating a session
type SessionConfig struct {
	Config          *Config
	Direction       CallDirection
	CallID          string // Optional: if empty, a new UUID will be generated
	Codec           *Codec
	Logger          commons.Logger
	Auth            interface{}             // Authentication principal (types.SimplePrinciple)
	Assistant       interface{}             // Assistant entity (*internal_assistant_entity.Assistant)
	VaultCredential *protos.VaultCredential // Vault-resolved SIP provider credential
}

// Session manages a single SIP call session
type Session struct {
	mu     sync.RWMutex
	logger commons.Logger

	info   SessionInfo
	config *Config
	ended  atomic.Bool

	ctx       context.Context
	cancel    context.CancelFunc
	eventChan chan Event
	errorChan chan error

	// RTP handling
	rtpHandler    *RTPHandler
	rtpLocalPort  int
	rtpRemoteAddr string
	rtpRemotePort int

	// Codec negotiation result
	negotiatedCodec *Codec

	// User metadata for passing context between layers (e.g., outbound call info)
	metadata map[string]interface{}

	// Authentication and authorization context - available in all session methods
	auth            interface{}             // Authentication principal (types.SimplePrinciple)
	assistant       interface{}             // Assistant entity (*internal_assistant_entity.Assistant)
	vaultCredential *protos.VaultCredential // Vault-resolved SIP provider credential

	// byeReceived is closed when a SIP BYE is received for this session.
	// Used to notify startCall about early BYE without fully ending the session.
	// This decouples BYE notification from session teardown, preventing the race
	// condition where handleOutboundDialog kills the session before startCall
	// has registered its own context cancellation.
	byeReceived     chan struct{}
	byeReceivedOnce sync.Once

	// Outbound dialog session — stored so BYE/re-INVITE handlers can access it.
	// nil for inbound calls.
	dialogClientSession *sipgo.DialogClientSession

	// Inbound dialog session — stored so we can send BYE when ending an inbound call.
	// nil for outbound calls.
	dialogServerSession *sipgo.DialogServerSession

	// onDisconnect is called via Disconnect() to perform transport-level call teardown
	// (e.g., sending SIP BYE). NOT called by End() — the caller must invoke
	// Disconnect() explicitly before End() if a SIP BYE should be sent.
	// Set by the server that owns this session.
	onDisconnect func(session *Session)
}

// NewSession creates a new SIP session
func NewSession(ctx context.Context, cfg *SessionConfig) (*Session, error) {
	if cfg.Config == nil {
		return nil, fmt.Errorf("%w: config is required", ErrInvalidConfig)
	}
	// Use ValidateRTP for inbound calls (no username/password needed)
	// Use full Validate for outbound calls
	if cfg.Direction == CallDirectionOutbound {
		if err := cfg.Config.Validate(); err != nil {
			return nil, err
		}
	} else {
		if err := cfg.Config.ValidateRTP(); err != nil {
			return nil, err
		}
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	callID := cfg.CallID
	if callID == "" {
		callID = uuid.New().String()
	}

	codec := cfg.Codec
	if codec == nil {
		codec = &CodecPCMU
	}

	session := &Session{
		logger: cfg.Logger,
		info: SessionInfo{
			CallID:     callID,
			LocalTag:   uuid.New().String()[:8],
			State:      CallStateInitializing,
			Direction:  cfg.Direction,
			StartTime:  time.Now(),
			Codec:      codec.Name,
			SampleRate: int(codec.ClockRate),
		},
		config:          cfg.Config,
		ctx:             sessionCtx,
		cancel:          cancel,
		eventChan:       make(chan Event, eventBufferSize),
		errorChan:       make(chan error, errorBufferSize),
		negotiatedCodec: codec,
		auth:            cfg.Auth,
		assistant:       cfg.Assistant,
		vaultCredential: cfg.VaultCredential,
		byeReceived:     make(chan struct{}),
	}

	return session, nil
}

// GetInfo returns the current session information
func (s *Session) GetInfo() SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info := s.info
	info.Duration = info.GetDuration()
	return info
}

// GetCallID returns the call ID
func (s *Session) GetCallID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info.CallID
}

// SetState updates the session state with proper state machine transitions
func (s *Session) SetState(state CallState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previousState := s.info.State

	// Validate state transitions
	if !s.isValidTransition(previousState, state) {
		if s.logger != nil {
			s.logger.Warnw("Invalid state transition",
				"call_id", s.info.CallID,
				"from", previousState,
				"to", state)
		}
		return
	}

	s.info.State = state

	switch state {
	case CallStateConnected:
		now := time.Now()
		s.info.ConnectedTime = &now
		s.emitEvent(EventTypeConnected, nil)
	case CallStateEnded:
		now := time.Now()
		s.info.EndTime = &now
		s.emitEvent(EventTypeBye, nil)
	case CallStateFailed:
		now := time.Now()
		s.info.EndTime = &now
		s.emitEvent(EventTypeError, nil)
	case CallStateRinging:
		s.emitEvent(EventTypeRinging, nil)
	}

	if s.logger != nil {
		s.logger.Debugw("Session state changed",
			"call_id", s.info.CallID,
			"from", previousState,
			"to", state)
	}
}

// isValidTransition checks if a state transition is valid
func (s *Session) isValidTransition(from, to CallState) bool {
	// Allow any transition to ended/failed
	if to == CallStateEnded || to == CallStateFailed {
		return true
	}

	// Prevent transitions from terminal states
	if from.IsTerminal() {
		return false
	}

	// Define valid transitions
	validTransitions := map[CallState][]CallState{
		CallStateInitializing: {CallStateRinging, CallStateConnected},
		CallStateRinging:      {CallStateConnected, CallStateEnding},
		CallStateConnected:    {CallStateOnHold, CallStateEnding},
		CallStateOnHold:       {CallStateConnected, CallStateEnding},
		CallStateEnding:       {CallStateEnded},
	}

	allowed, exists := validTransitions[from]
	if !exists {
		return false
	}

	for _, validTo := range allowed {
		if validTo == to {
			return true
		}
	}
	return false
}

// emitEvent sends an event to the event channel (non-blocking).
// Safe to call during End() — the recover guard handles closed channel.
func (s *Session) emitEvent(eventType EventType, data map[string]interface{}) {
	event := NewEvent(eventType, s.info.CallID, data)
	defer func() { recover() }()
	select {
	case s.eventChan <- event:
	default:
	}
}

// SetRemoteRTP sets the remote RTP address after SDP negotiation
func (s *Session) SetRemoteRTP(addr string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rtpRemoteAddr = addr
	s.rtpRemotePort = port
	s.info.RemoteRTPAddress = fmt.Sprintf("%s:%d", addr, port)
}

// SetLocalRTP sets the local RTP address
func (s *Session) SetLocalRTP(addr string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rtpLocalPort = port
	s.info.LocalRTPAddress = fmt.Sprintf("%s:%d", addr, port)
}

// GetLocalRTP returns the local RTP IP and port for this session.
func (s *Session) GetLocalRTP() (string, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Parse IP from the stored LocalRTPAddress ("ip:port" format)
	addr := s.info.LocalRTPAddress
	if addr == "" {
		return "", s.rtpLocalPort
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, s.rtpLocalPort
	}
	return host, s.rtpLocalPort
}

// GetRTPLocalPort returns the local RTP port bound for this session.
func (s *Session) GetRTPLocalPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rtpLocalPort
}

// SetNegotiatedCodec sets the negotiated codec
func (s *Session) SetNegotiatedCodec(codecName string, sampleRate int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	codec := GetCodecByName(codecName)
	if codec == nil {
		codec = &CodecPCMU
	}
	s.negotiatedCodec = codec
	s.info.Codec = codec.Name
	s.info.SampleRate = sampleRate
}

// GetNegotiatedCodec returns the negotiated codec
func (s *Session) GetNegotiatedCodec() *Codec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.negotiatedCodec
}

// SetRTPHandler sets the RTP handler for this session.
// The Streamer reads/writes directly from/to the RTP handler's audio channels.
func (s *Session) SetRTPHandler(handler *RTPHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rtpHandler = handler
}

// GetRTPHandler returns the RTP handler for this session
func (s *Session) GetRTPHandler() *RTPHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rtpHandler
}

// Events returns the event channel
func (s *Session) Events() <-chan Event {
	return s.eventChan
}

// Errors returns the error channel
func (s *Session) Errors() <-chan error {
	return s.errorChan
}

// Context returns the session context
func (s *Session) Context() context.Context {
	return s.ctx
}

// SetMetadata stores a key-value pair on the session
func (s *Session) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metadata == nil {
		s.metadata = make(map[string]interface{})
	}
	s.metadata[key] = value
}

// GetMetadata retrieves a value by key from session metadata
func (s *Session) GetMetadata(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.metadata == nil {
		return nil, false
	}
	v, ok := s.metadata[key]
	return v, ok
}

// SetDialogClientSession stores the outbound DialogClientSession on this session.
// This allows BYE and re-INVITE handlers to interact with the sipgo dialog.
func (s *Session) SetDialogClientSession(ds *sipgo.DialogClientSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialogClientSession = ds
}

// GetDialogClientSession returns the outbound DialogClientSession, or nil for inbound calls.
func (s *Session) GetDialogClientSession() *sipgo.DialogClientSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dialogClientSession
}

// SetDialogServerSession stores the inbound DialogServerSession on this session.
// This allows the server to send BYE when ending an inbound call.
func (s *Session) SetDialogServerSession(ds *sipgo.DialogServerSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialogServerSession = ds
}

// GetDialogServerSession returns the inbound DialogServerSession, or nil for outbound calls.
func (s *Session) GetDialogServerSession() *sipgo.DialogServerSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dialogServerSession
}

// SetOnDisconnect registers a callback that is invoked when the session is disconnected.
// This allows the SIP server to inject transport-level call teardown (e.g., sending BYE)
// without the session needing to know about SIP signaling internals.
func (s *Session) SetOnDisconnect(fn func(session *Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDisconnect = fn
}

// Disconnect performs transport-level call teardown by invoking the onDisconnect callback.
// This sends a SIP BYE (or equivalent) to the remote party before local cleanup.
// Safe to call multiple times — the callback is cleared after first invocation.
func (s *Session) Disconnect() {
	s.mu.Lock()
	fn := s.onDisconnect
	s.onDisconnect = nil // Clear to prevent double-disconnect
	s.mu.Unlock()

	if fn != nil {
		fn(s)
	}
}

// GetAuth returns the authentication principal (types.SimplePrinciple) for this session.
// Available in all session methods after session creation.
func (s *Session) GetAuth() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auth
}

// GetAssistant returns the assistant entity for this session.
// Available in all session methods after session creation.
func (s *Session) GetAssistant() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.assistant
}

// GetVaultCredential returns the vault-resolved SIP provider credential for this session.
// Available in all session methods after session creation.
func (s *Session) GetVaultCredential() *protos.VaultCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vaultCredential
}

// SendEvent sends an event notification (non-blocking)
func (s *Session) SendEvent(event Event) {
	if s.ended.Load() {
		return
	}
	defer func() { recover() }()
	select {
	case s.eventChan <- event:
	default:
		// Event dropped if channel is full
	}
}

// SendError sends an error to the error channel (non-blocking)
func (s *Session) SendError(err error) {
	if s.ended.Load() {
		return
	}
	defer func() { recover() }()
	select {
	case s.errorChan <- err:
	default:
		// Error dropped if channel is full
	}
}

// End terminates the SIP session gracefully
func (s *Session) End() {
	// Use atomic to ensure End is only called once
	if !s.ended.CompareAndSwap(false, true) {
		return // Already ended
	}

	s.mu.RLock()
	terminal := s.info.State.IsTerminal()
	s.mu.RUnlock()
	if !terminal {
		s.SetState(CallStateEnding)
	}

	s.mu.Lock()
	rtpHandler := s.rtpHandler
	s.rtpHandler = nil
	s.mu.Unlock()

	if rtpHandler != nil {
		if err := rtpHandler.Stop(); err != nil && s.logger != nil {
			s.logger.Warnw("Error stopping RTP handler", "error", err, "call_id", s.info.CallID)
		}
	}

	s.cancel()

	s.mu.RLock()
	terminal = s.info.State.IsTerminal()
	s.mu.RUnlock()
	if !terminal {
		s.SetState(CallStateEnded)
	}

	// Close channels safely
	s.closeChannels()

	if s.logger != nil {
		s.logger.Info("Session ended",
			"call_id", s.info.CallID,
			"duration", s.info.GetDuration())
	}
}

// closeChannels safely closes all session channels
func (s *Session) closeChannels() {
	defer func() {
		// Recover from panic if channel is already closed
		recover()
	}()

	close(s.eventChan)
	close(s.errorChan)
}

// IsActive returns whether the session is still active
func (s *Session) IsActive() bool {
	if s.ended.Load() {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info.State.IsActive()
}

// IsEnded returns whether the session has ended
func (s *Session) IsEnded() bool {
	return s.ended.Load()
}

// NotifyBye signals that a SIP BYE has been received for this session.
// This is safe to call multiple times — only the first call has effect.
// It does NOT end the session; it merely notifies listeners (e.g., startCall)
// that a BYE was received so they can shut down gracefully.
func (s *Session) NotifyBye() {
	s.byeReceivedOnce.Do(func() {
		close(s.byeReceived)
	})
}

// ByeReceived returns a channel that is closed when a SIP BYE is received.
// Use this in select{} to detect early BYE without relying on session.End().
func (s *Session) ByeReceived() <-chan struct{} {
	return s.byeReceived
}

// GetConfig returns the SIP configuration for this session.
func (s *Session) GetConfig() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// GetState returns the current session state
func (s *Session) GetState() CallState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info.State
}

// GetRTPStats returns RTP statistics if available
func (s *Session) GetRTPStats() *RTPStats {
	s.mu.RLock()
	rtpHandler := s.rtpHandler
	s.mu.RUnlock()

	if rtpHandler == nil {
		return nil
	}

	sent, received := rtpHandler.GetStats()
	return &RTPStats{
		PacketsSent:     sent,
		PacketsReceived: received,
	}
}
