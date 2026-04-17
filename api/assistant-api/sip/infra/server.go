// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_infra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/types"
	"github.com/rapidaai/protos"
	"github.com/redis/go-redis/v9"
)

// ServerState represents the state of the SIP server
type ServerState int32

const (
	ServerStateCreated ServerState = iota
	ServerStateRunning
	ServerStateStopped
)

// SIPRequestContext contains information about an incoming SIP request.
// Used by the middleware chain to resolve config for every SIP request.
//
// Middleware enriches this context as it flows through the chain:
//
//	routingMiddleware   → resolves assistant by DID lookup, sets Extra["auth"]
//	assistantMiddleware → loads assistant entity, sets Extra["assistant"]
//	vaultConfigResolver → fetches SIP config from vault, sets Extra["sip_config"]
type SIPRequestContext struct {
	Method  string // SIP method (INVITE, REGISTER, BYE, etc.)
	CallID  string
	FromURI string
	ToURI   string
	SDPInfo *SDPMediaInfo

	// Authentication fields extracted from URI userinfo
	// Parsed from: sip:{assistantID}:{apiKey}@host
	APIKey      string // API key (password part of userinfo)
	AssistantID string // Assistant ID (user part of userinfo)

	// Extra holds middleware-resolved state (auth principal, assistant entity, etc.).
	// Using interface{} keeps the infra package decoupled from business types.
	// Keys: "auth" → types.SimplePrinciple, "assistant" → *Assistant, "sip_config" → *Config
	Extra map[string]interface{}
}

// Set stores a value in the middleware context.
func (c *SIPRequestContext) Set(key string, value interface{}) {
	if c.Extra == nil {
		c.Extra = make(map[string]interface{})
	}
	c.Extra[key] = value
}

// Get retrieves a value from the middleware context.
func (c *SIPRequestContext) Get(key string) (interface{}, bool) {
	if c.Extra == nil {
		return nil, false
	}
	v, ok := c.Extra[key]
	return v, ok
}

// InviteResult contains the resolved configuration for handling the call
type InviteResult struct {
	Config      *Config // Tenant-specific config (RTP ports, credentials, etc.)
	ShouldAllow bool    // Whether to accept the call
	RejectCode  int     // SIP response code if rejecting (e.g., 403, 404)
	RejectMsg   string  // Optional message for rejection

	// Extra carries middleware-resolved state (auth, assistant, etc.) back to the
	// infra layer so it can be stored as session metadata. The server copies this
	// map onto the Session after creation, making it available to onInvite handlers.
	Extra map[string]interface{}
}

// Reject creates an InviteResult that rejects the call with the given SIP code and message.
func Reject(code int, msg string) *InviteResult {
	return &InviteResult{ShouldAllow: false, RejectCode: code, RejectMsg: msg}
}

// Allow creates an InviteResult that accepts the call with the resolved config.
func Allow(config *Config) *InviteResult {
	return &InviteResult{ShouldAllow: true, Config: config}
}

// AllowWithExtra creates an InviteResult that accepts the call and carries
// resolved state (auth principal, assistant entity, etc.) so the infra layer
// can propagate it to session metadata.
func AllowWithExtra(config *Config, extra map[string]interface{}) *InviteResult {
	return &InviteResult{ShouldAllow: true, Config: config, Extra: extra}
}

// ConfigResolver resolves tenant-specific config from a SIP request.
// Returns an InviteResult with Config (for RTP/SIP setup) and optionally Extra
// (auth, assistant, etc. to be stored as session metadata).
type ConfigResolver func(ctx *SIPRequestContext) (*InviteResult, error)

// Middleware processes a SIP request context and either enriches it or rejects it.
// Each middleware receives the context, enriches it (e.g., sets auth, assistant),
// and calls next() to continue the chain. Returning an InviteResult with
// ShouldAllow=false short-circuits the chain.
//
// Example chain for INVITE:
//
//	routingMiddleware → assistantMiddleware → vaultConfigResolver
type Middleware func(ctx *SIPRequestContext, next func() (*InviteResult, error)) (*InviteResult, error)

// MiddlewareChain composes a slice of Middleware into a single ConfigResolver.
// The chain executes each middleware in order; the final handler is called
// if all middlewares pass. This replaces the monolithic ConfigResolver approach
// with composable, testable middleware functions.
func MiddlewareChain(middlewares []Middleware, final ConfigResolver) ConfigResolver {
	return func(ctx *SIPRequestContext) (*InviteResult, error) {
		var run func(i int) (*InviteResult, error)
		run = func(i int) (*InviteResult, error) {
			if i >= len(middlewares) {
				return final(ctx)
			}
			return middlewares[i](ctx, func() (*InviteResult, error) {
				return run(i + 1)
			})
		}
		return run(0)
	}
}

// Server wraps sipgo for handling SIP signaling
// Uses native SIP signaling (UDP/TCP/TLS) - no WebSocket needed
// Multi-tenant: Config is resolved per-call via ConfigResolver callback
type Server struct {
	mu     sync.RWMutex
	logger commons.Logger
	state  atomic.Int32

	ua           *sipgo.UserAgent
	server       *sipgo.Server
	client       *sipgo.Client
	listenConfig *ListenConfig     // Shared server listen config (address, port, transport)
	rtpAllocator *RTPPortAllocator // Allocates RTP ports from configured range

	// Outbound dialog cache — routes incoming BYE/re-INVITE to the correct
	// DialogClientSession. Without this, BYE from the remote side is handled
	// only at the Session level and the sipgo dialog stays in limbo.
	dialogClientCache *sipgo.DialogClientCache

	// Inbound dialog cache — manages UAS dialog state for inbound calls so we
	// can send BYE when the assistant ends the conversation. Without this,
	// ending an inbound call only does local cleanup and the remote PBX keeps
	// the call alive until timeout.
	dialogServerCache *sipgo.DialogServerCache

	sessions     map[string]*Session
	sessionCount atomic.Int64

	// Multi-tenant config resolver - called for each incoming INVITE
	configResolver ConfigResolver

	// Event callbacks
	onInvite func(session *Session, fromURI, toURI string) error
	onBye    func(session *Session) error
	onCancel func(session *Session) error
	onError  func(session *Session, err error)

	ctx    context.Context
	cancel context.CancelFunc
}

// ListenConfig holds shared server configuration (not tenant-specific)
type ListenConfig struct {
	Address    string    `json:"address" mapstructure:"address"`         // Bind address (e.g. 0.0.0.0)
	ExternalIP string    `json:"external_ip" mapstructure:"external_ip"` // Public/reachable IP for SDP and Contact headers
	Port       int       `json:"port" mapstructure:"port"`
	Transport  Transport `json:"transport" mapstructure:"transport"`
}

// GetExternalIP returns the external/advertised IP for SDP and SIP Contact headers.
// ExternalIP must be explicitly configured (SIP__EXTERNAL_IP) for production use.
// Falls back to Address only if ExternalIP is not set.
func (c *ListenConfig) GetExternalIP() string {
	if c.ExternalIP != "" {
		return c.ExternalIP
	}
	return c.Address
}

// GetBindAddress returns the address to bind RTP sockets to.
// This is the actual local interface address (e.g. 0.0.0.0) — NOT the
// external/public IP. RTP sockets must bind to a local interface, while
// the external IP is only advertised in SDP so the remote peer knows
// where to send its RTP packets.
func (c *ListenConfig) GetBindAddress() string {
	return c.Address
}

// GetListenAddr returns the address to listen on
func (c *ListenConfig) GetListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Address, c.Port)
}

// ServerConfig holds configuration for creating a SIP server
// Multi-tenant: Only holds shared listen config, tenant config resolved per-call
type ServerConfig struct {
	ListenConfig      *ListenConfig  // Shared server listen configuration
	ConfigResolver    ConfigResolver // Resolves tenant-specific config per-call
	Logger            commons.Logger
	RedisClient       *redis.Client // Redis client for distributed RTP port allocation
	RTPPortRangeStart int           // Start of RTP port range (even, >= 1024)
	RTPPortRangeEnd   int           // End of RTP port range (exclusive)
}

// Validate validates the server configuration
func (c *ServerConfig) Validate() error {
	if c.ListenConfig == nil {
		return fmt.Errorf("listen config is required")
	}
	if c.ListenConfig.Address == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.ListenConfig.Port <= 0 || c.ListenConfig.Port > 65535 {
		return fmt.Errorf("invalid listen port: %d", c.ListenConfig.Port)
	}
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if c.RedisClient == nil {
		return fmt.Errorf("redis client is required for distributed RTP port allocation")
	}
	if c.RTPPortRangeStart <= 0 || c.RTPPortRangeEnd <= 0 {
		return fmt.Errorf("rtp_port_range must be specified")
	}
	if c.RTPPortRangeStart >= c.RTPPortRangeEnd {
		return fmt.Errorf("rtp_port_range_start must be less than rtp_port_range_end")
	}
	return nil
}

// NewServer creates a new shared SIP server instance
// Multi-tenant: Server listens on shared address, config resolved per-call via ConfigResolver
func NewServer(ctx context.Context, cfg *ServerConfig) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, NewSIPError("NewServer", "", "configuration validation failed", err)
	}

	serverCtx, cancel := context.WithCancel(ctx)

	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent("RapidaVoiceAI"),
		sipgo.WithUserAgentTransactionLayerOptions(
			sip.WithTransactionLayerUnhandledResponseHandler(func(r *sip.Response) {
				// Absorb retransmissions silently — these are expected when
				// the remote side retransmits responses before the transaction completes
				cfg.Logger.Debug("Unhandled SIP response (retransmission)",
					"status", r.StatusCode,
					"reason", r.Reason)
			}),
		),
	)
	if err != nil {
		cancel()
		return nil, NewSIPError("NewServer", "", "failed to create SIP user agent", err)
	}

	server, err := sipgo.NewServer(ua)
	if err != nil {
		cancel()
		return nil, NewSIPError("NewServer", "", "failed to create SIP server", err)
	}

	// Log full ListenConfig so config issues are immediately visible
	resolvedIP := cfg.ListenConfig.GetExternalIP()
	cfg.Logger.Infow("SIP server config",
		"bind_address", cfg.ListenConfig.Address,
		"external_ip_config", cfg.ListenConfig.ExternalIP,
		"external_ip_resolved", resolvedIP,
		"port", cfg.ListenConfig.Port,
		"transport", cfg.ListenConfig.Transport)
	if resolvedIP == "" || resolvedIP == "0.0.0.0" || resolvedIP == "::" {
		cfg.Logger.Warn("SIP ExternalIP not configured — SDP will advertise bind address which may be unroutable. Set SIP__EXTERNAL_IP in config.")
	}

	// Use the external/public IP for SIP Via/Contact headers so remote peers can reach us
	clientOpts := []sipgo.ClientOption{
		sipgo.WithClientHostname(resolvedIP),
	}
	if cfg.ListenConfig.Port > 0 {
		clientOpts = append(clientOpts, sipgo.WithClientPort(cfg.ListenConfig.Port))
	}

	client, err := sipgo.NewClient(ua, clientOpts...)
	if err != nil {
		cancel()
		return nil, NewSIPError("NewServer", "", "failed to create SIP client", err)
	}

	// Create Redis-backed distributed RTP port allocator
	rtpAllocator := NewRTPPortAllocator(cfg.RedisClient, cfg.Logger, cfg.RTPPortRangeStart, cfg.RTPPortRangeEnd)
	if err := rtpAllocator.Init(serverCtx); err != nil {
		cancel()
		return nil, NewSIPError("NewServer", "", "failed to initialize RTP port allocator", err)
	}

	// Build the Contact header used for outbound dialog sessions.
	// Uses the external IP so the remote side can route subsequent requests back to us.
	contactHDR := sip.ContactHeader{
		Address: sip.Uri{
			Scheme: "sip",
			Host:   cfg.ListenConfig.GetExternalIP(),
			Port:   cfg.ListenConfig.Port,
		},
	}

	// Create dialog client cache — routes incoming BYE/re-INVITE for outbound dialogs
	// to the correct DialogClientSession. This is essential for proper dialog lifecycle:
	// without it, BYE from the remote side never terminates the sipgo dialog, and
	// re-INVITE responses lack proper dialog context (Contact, To-tag).
	dialogClientCache := sipgo.NewDialogClientCache(client, contactHDR)

	// Create dialog server cache — manages UAS dialog state for inbound calls.
	// This allows us to send BYE when the assistant ends an inbound conversation,
	// properly tearing down the call on the remote PBX side.
	dialogServerCache := sipgo.NewDialogServerCache(client, contactHDR)

	s := &Server{
		logger:            cfg.Logger,
		ua:                ua,
		server:            server,
		client:            client,
		listenConfig:      cfg.ListenConfig,
		rtpAllocator:      rtpAllocator,
		dialogClientCache: dialogClientCache,
		dialogServerCache: dialogServerCache,
		configResolver:    cfg.ConfigResolver,
		sessions:          make(map[string]*Session),
		ctx:               serverCtx,
		cancel:            cancel,
	}

	s.state.Store(int32(ServerStateCreated))
	s.registerHandlers()

	return s, nil
}

func (s *Server) registerHandlers() {
	s.server.OnInvite(s.handleInvite)
	s.server.OnAck(s.handleAck)
	s.server.OnBye(s.handleBye)
	s.server.OnCancel(s.handleCancel)
	s.server.OnRegister(s.handleRegister)
	s.server.OnOptions(s.handleOptions)

	// Handle UPDATE — Asterisk sends UPDATE for direct_media negotiation and session timers.
	// Without this handler, sipgo responds 405 Method Not Allowed, which causes Asterisk to
	// tear down the bridge (the exact symptom: call disconnects ~2ms after answer).
	s.server.OnUpdate(s.handleUpdate)

	// Handle INFO — some PBXes send INFO for DTMF relay (RFC 2833) or session information.
	s.server.OnInfo(s.handleInfo)

	// Handle NOTIFY — sent for REFER progress, subscription events, and MWI.
	s.server.OnNotify(s.handleNotify)

	// Handle REFER — call transfer requests from the remote side.
	s.server.OnRefer(s.handleRefer)

	// Handle SUBSCRIBE — Twilio sends SUBSCRIBE for dialog-info and presence events.
	// Reject cleanly to prevent retry loops.
	s.server.OnSubscribe(s.handleSubscribe)

	// Handle MESSAGE — FreeSWITCH sends MESSAGE for T.38 fax or text-based events.
	s.server.OnMessage(s.handleMessage)

	// Catch-all for any SIP method we don't explicitly handle. Without this,
	// sipgo responds 405 Method Not Allowed which can cause Asterisk to tear down calls.
	// For in-dialog requests (known Call-ID), respond 200 OK to keep the dialog alive.
	// For out-of-dialog requests, respond 405 as before.
	s.server.OnNoRoute(s.handleUnknownRequest)
}

// Start begins listening for SIP traffic
func (s *Server) Start() error {
	if !s.state.CompareAndSwap(int32(ServerStateCreated), int32(ServerStateRunning)) {
		return fmt.Errorf("server is not in created state")
	}

	listenAddr := s.listenConfig.GetListenAddr()
	transport := s.listenConfig.Transport.String()
	if transport == "" {
		transport = "udp"
	}

	go func() {
		err := s.server.ListenAndServe(s.ctx, transport, listenAddr)
		if err != nil && s.state.Load() == int32(ServerStateRunning) {
			s.logger.Errorw("SIP server stopped unexpectedly",
				"error", err,
				"address", listenAddr)
			s.state.Store(int32(ServerStateStopped))
		}
	}()

	s.logger.Infow("SIP server started (multi-tenant)",
		"address", listenAddr,
		"transport", transport)

	return nil
}

// Stop stops the SIP server gracefully
func (s *Server) Stop() {
	if !s.state.CompareAndSwap(int32(ServerStateRunning), int32(ServerStateStopped)) {
		return // Already stopped or not running
	}

	s.logger.Infow("Stopping SIP server")

	// Cancel context first to stop accepting new calls
	s.cancel()

	// End all active sessions
	s.mu.Lock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessions = make(map[string]*Session)
	s.mu.Unlock()

	for _, session := range sessions {
		session.End()
	}

	// Release all RTP ports allocated by this instance back to Redis
	s.rtpAllocator.ReleaseAll(context.Background())

	s.logger.Infow("SIP server stopped", "sessions_ended", len(sessions))
}

// SetConfigResolver sets the callback for resolving tenant-specific config.
// For middleware-based auth, use SetMiddlewares instead.
func (s *Server) SetConfigResolver(resolver ConfigResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configResolver = resolver
}

// SetMiddlewares composes a middleware chain with a final handler and sets it
// as the config resolver. This is the preferred way to set up authentication
// for all SIP requests.
//
// Example:
//
//	server.SetMiddlewares(
//	    []Middleware{routingMiddleware, assistantMiddleware},
//	    vaultConfigResolver,
//	)
func (s *Server) SetMiddlewares(middlewares []Middleware, final ConfigResolver) {
	s.SetConfigResolver(MiddlewareChain(middlewares, final))
}

// IsRunning returns true if the server is running
func (s *Server) IsRunning() bool {
	return s.state.Load() == int32(ServerStateRunning)
}

// AllocateRTPPort allocates an available RTP port from the shared pool.
// Callers must call ReleaseRTPPort when the port is no longer needed.
func (s *Server) AllocateRTPPort() (int, error) {
	return s.rtpAllocator.Allocate()
}

// ReleaseRTPPort returns an RTP port to the shared pool.
func (s *Server) ReleaseRTPPort(port int) {
	s.rtpAllocator.Release(port)
}

// Client returns the underlying sipgo client for outbound requests (e.g., REGISTER).
func (s *Server) Client() *sipgo.Client {
	return s.client
}

// ListenConfig returns the shared server listen configuration.
func (s *Server) GetListenConfig() *ListenConfig {
	return s.listenConfig
}

// SessionCount returns the number of active sessions
func (s *Server) SessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// SetOnInvite sets the callback for incoming INVITE requests
func (s *Server) SetOnInvite(fn func(session *Session, fromURI, toURI string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onInvite = fn
}

// SetOnBye sets the callback for BYE requests
func (s *Server) SetOnBye(fn func(session *Session) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onBye = fn
}

// SetOnCancel sets the callback for CANCEL requests
func (s *Server) SetOnCancel(fn func(session *Session) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onCancel = fn
}

// SetOnError sets the callback for error events
func (s *Server) SetOnError(fn func(session *Session, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onError = fn
}

func (s *Server) handleInvite(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	fromURI := req.From().Address.String()
	toURI := req.To().Address.String()

	s.logger.Infow("Received INVITE", "call_id", callID, "from", fromURI, "to", toURI)

	// Check if this is a re-INVITE for an existing session (e.g., codec renegotiation
	// or hold/resume from remote side, common after outbound calls are answered)
	s.mu.RLock()
	existingSession, isReInvite := s.sessions[callID]
	s.mu.RUnlock()

	if isReInvite && existingSession != nil {
		info := existingSession.GetInfo()
		s.logger.Infow("Routing as re-INVITE for existing session",
			"call_id", callID,
			"direction", info.Direction,
			"state", info.State)
		s.handleReInvite(req, tx, existingSession)
		return
	}

	// Parse SDP from incoming INVITE to get remote RTP address and codec preferences
	sdpInfo, err := s.ParseSDP(req.Body())
	if err != nil {
		s.logger.Warnw("Failed to parse SDP, using defaults", "error", err, "call_id", callID)
		sdpInfo = &SDPMediaInfo{PreferredCodec: &CodecPCMU}
	}

	// Resolve tenant-specific config via middleware chain.
	// The chain: routingMiddleware → assistantMiddleware → vaultConfigResolver
	// Each middleware enriches the SIPRequestContext; the final handler returns the InviteResult.
	s.mu.RLock()
	resolver := s.configResolver
	s.mu.RUnlock()

	var tenantConfig *Config
	var resolvedExtra map[string]interface{}

	if resolver != nil {
		reqCtx := &SIPRequestContext{
			Method:  "INVITE",
			CallID:  callID,
			FromURI: fromURI,
			ToURI:   toURI,
			SDPInfo: sdpInfo,
		}
		result, err := resolver(reqCtx)
		if err != nil {
			s.logger.Errorw("SIP authentication/config resolution failed", "error", err, "call_id", callID)
			s.sendResponse(tx, req, 500)
			return
		}
		if !result.ShouldAllow {
			s.logger.Warnw("Call rejected by authentication chain",
				"call_id", callID,
				"code", result.RejectCode,
				"reason", result.RejectMsg)
			s.sendResponse(tx, req, result.RejectCode)
			return
		}
		tenantConfig = result.Config
		resolvedExtra = result.Extra

		s.logger.Debugw("SIP INVITE authenticated",
			"call_id", callID,
			"assistant_id", reqCtx.AssistantID,
			"has_api_key", reqCtx.APIKey != "")
	}

	// Reject if no config was resolved — all config must be explicitly provided
	if tenantConfig == nil {
		s.logger.Errorw("No SIP config resolved for call, rejecting", "call_id", callID)
		s.sendResponse(tx, req, 500)
		return
	}

	// For inbound calls, ensure the server address is set from listen config
	// so RTP handler binds to the correct local IP
	if tenantConfig.Server == "" || tenantConfig.Server == "0.0.0.0" {
		tenantConfig.Server = s.listenConfig.GetExternalIP()
	}

	// Negotiate codec
	negotiatedCodec := sdpInfo.PreferredCodec
	if negotiatedCodec == nil {
		negotiatedCodec = &CodecPCMU
	}

	// Extract vault credential from resolved extra for direct session access
	var vaultCredential *protos.VaultCredential
	if vaultCredVal, ok := resolvedExtra["vault_credential"]; ok {
		if vaultCred, ok := vaultCredVal.(*protos.VaultCredential); ok {
			vaultCredential = vaultCred
		}
	}

	// Create session with resolved tenant config and middleware state
	auth, _ := resolvedExtra["auth"].(types.SimplePrinciple)
	assistant, _ := resolvedExtra["assistant"].(*internal_assistant_entity.Assistant)
	session, err := NewSession(s.ctx, &SessionConfig{
		Config:          tenantConfig,
		Direction:       CallDirectionInbound,
		CallID:          callID,
		Codec:           negotiatedCodec,
		Logger:          s.logger,
		Auth:            auth,
		Assistant:       assistant,
		VaultCredential: vaultCredential,
	})
	if err != nil {
		s.logger.Errorw("Failed to create session", "error", err, "call_id", callID)
		s.sendResponse(tx, req, 500)
		return
	}

	// Also propagate all middleware-resolved state to metadata for backward compatibility
	// so the onInvite handler can access it via session.GetMetadata() if needed.
	for k, v := range resolvedExtra {
		session.SetMetadata(k, v)
	}

	// Register session
	s.mu.Lock()
	s.sessions[callID] = session
	s.sessionCount.Add(1)
	s.mu.Unlock()

	// Create an inbound dialog session via the server dialog cache.
	// This tracks dialog state (To-tag, CSeq, Route) so we can later send
	// BYE to properly disconnect the call when the assistant ends the conversation.
	dialogSession, err := s.dialogServerCache.ReadInvite(req, tx)
	if err != nil {
		s.logger.Warnw("Failed to create inbound dialog session — BYE on disconnect will not work",
			"error", err, "call_id", callID)
		// Fall back to non-dialog response flow
		s.sendResponse(tx, req, 100)
		s.sendResponse(tx, req, 180)
	} else {
		session.SetDialogServerSession(dialogSession)
		// Send provisionals via dialog session (non-blocking, ensures consistent To-tag)
		if err := dialogSession.Respond(100, "Trying", nil); err != nil {
			s.logger.Warnw("Failed to send 100 via dialog", "error", err, "call_id", callID)
		}
		if err := dialogSession.Respond(180, "Ringing", nil); err != nil {
			s.logger.Warnw("Failed to send 180 via dialog", "error", err, "call_id", callID)
		}
	}
	session.SetState(CallStateRinging)

	s.logger.Debugw("Parsed remote SDP",
		"call_id", callID,
		"remote_rtp_ip", sdpInfo.ConnectionIP,
		"remote_rtp_port", sdpInfo.AudioPort,
		"codec", negotiatedCodec.Name)

	// Allocate an RTP port from the shared pool
	rtpPort, err := s.rtpAllocator.Allocate()
	if err != nil {
		s.logger.Errorw("No RTP ports available", "error", err, "call_id", callID)
		s.removeSession(callID)
		s.sendResponse(tx, req, 503) // Service Unavailable
		return
	}

	// Create RTP handler with allocated port — bind to the local/bind address
	// (0.0.0.0), not the external IP. The external IP is only for SDP
	// advertisement. Binding to an external IP that isn't on a local interface
	// causes net.ListenUDP to fail.
	rtpBindIP := s.listenConfig.GetBindAddress()
	rtpHandler, err := NewRTPHandler(s.ctx, &RTPConfig{
		LocalIP:     rtpBindIP,
		LocalPort:   rtpPort,
		PayloadType: negotiatedCodec.PayloadType,
		ClockRate:   negotiatedCodec.ClockRate,
		Logger:      s.logger,
	})
	if err != nil {
		s.rtpAllocator.Release(rtpPort)
		s.logger.Errorw("Failed to create RTP handler", "error", err, "call_id", callID)
		s.removeSession(callID)
		s.sendResponse(tx, req, 500)
		return
	}

	// Set remote RTP address from incoming SDP
	if sdpInfo.ConnectionIP != "" && sdpInfo.AudioPort > 0 {
		rtpHandler.SetRemoteAddr(sdpInfo.ConnectionIP, sdpInfo.AudioPort)
		session.SetRemoteRTP(sdpInfo.ConnectionIP, sdpInfo.AudioPort)
	}

	// Get local RTP address — use external IP for SDP so remote peer sends RTP to reachable address
	_, localPort := rtpHandler.LocalAddr()
	externalIP := s.listenConfig.GetExternalIP()
	session.SetLocalRTP(externalIP, localPort)
	session.SetNegotiatedCodec(negotiatedCodec.Name, int(negotiatedCodec.ClockRate))

	// Store the RTP handler in the session
	session.SetRTPHandler(rtpHandler)

	// Start RTP processing
	rtpHandler.Start()

	// Generate SDP for response — advertise the negotiated codec only.
	// Using NegotiatedSDPConfig ensures we confirm the codec we agreed upon,
	// rather than re-offering all codecs which can confuse some PBXes.
	sdpConfig := s.NegotiatedSDPConfig(externalIP, localPort, negotiatedCodec)
	sdpBody := s.GenerateSDP(sdpConfig)

	// Send 200 OK with SDP.
	// When a dialog session exists, use RespondSDP which blocks until ACK is
	// received (or timeout). This establishes the dialog in Confirmed state,
	// enabling us to send BYE later. Falls back to manual response if no dialog.
	if ds := session.GetDialogServerSession(); ds != nil {
		if err := ds.RespondSDP([]byte(sdpBody)); err != nil {
			s.logger.Warnw("Dialog RespondSDP failed — falling back to manual response",
				"error", err, "call_id", callID)
			s.sendResponseWithSDPBody(tx, req, sdpBody)
		}
	} else {
		s.sendResponseWithSDPBody(tx, req, sdpBody)
	}
	session.SetState(CallStateConnected)

	// Register the onDisconnect callback — sends SIP BYE and removes from session map.
	// Called by session.End() before RTP/context cleanup. Does NOT call session.End()
	// (that would recurse). The session owns the full teardown sequence.
	session.SetOnDisconnect(func(sess *Session) {
		s.sendBye(sess)
		s.removeSession(callID)
	})

	s.logger.Infow("SIP call answered",
		"call_id", callID,
		"local_rtp", fmt.Sprintf("%s:%d", externalIP, localPort),
		"remote_rtp", fmt.Sprintf("%s:%d", sdpInfo.ConnectionIP, sdpInfo.AudioPort),
		"codec", negotiatedCodec.Name)

	// Call the invite handler (which will start the conversation)
	s.mu.RLock()
	onInvite := s.onInvite
	s.mu.RUnlock()

	if onInvite != nil {
		if err := onInvite(session, fromURI, toURI); err != nil {
			s.logger.Errorw("INVITE handler failed", "error", err, "call_id", callID)
			s.notifyError(session, err)
		}
	}
}

// removeSession removes a session from the sessions map and releases its RTP port.
func (s *Server) removeSession(callID string) {
	s.mu.Lock()
	session, exists := s.sessions[callID]
	if exists {
		delete(s.sessions, callID)
		s.sessionCount.Add(-1)
	}
	s.mu.Unlock()

	// Release the RTP port back to the pool
	if exists && session != nil {
		if port := session.GetRTPLocalPort(); port > 0 {
			s.rtpAllocator.Release(port)
		}
	}
}

// notifyError notifies the error handler if set
func (s *Server) notifyError(session *Session, err error) {
	s.mu.RLock()
	onError := s.onError
	s.mu.RUnlock()

	if onError != nil {
		onError(session, err)
	}
}

// handleReInvite processes a re-INVITE for an existing session.
// Re-INVITEs are sent by the remote side for:
//   - Codec renegotiation (all providers)
//   - Hold/resume (Twilio: sendonly/inactive, Asterisk: 0.0.0.0, Vonage: inactive)
//   - Direct media / session refresh (Asterisk, FreeSWITCH)
//   - ICE restart (WebRTC-based providers)
//
// We update the remote RTP address only when the SDP represents active media.
// Hold signals (0.0.0.0, sendonly, inactive) are acknowledged but don't redirect RTP.
func (s *Server) handleReInvite(req *sip.Request, tx sip.ServerTransaction, session *Session) {
	callID := req.CallID().Value()
	info := session.GetInfo()
	s.logger.Infow("Handling re-INVITE for existing session",
		"call_id", callID,
		"direction", info.Direction)

	// For outbound calls, validate the re-INVITE through the dialog cache.
	// This updates the remoteCSeqNo in the dialog so subsequent requests are accepted.
	if info.Direction == CallDirectionOutbound {
		if dialogSession := session.GetDialogClientSession(); dialogSession != nil {
			if err := dialogSession.ReadRequest(req, tx); err != nil {
				s.logger.Warnw("re-INVITE CSeq validation failed through dialog",
					"error", err, "call_id", callID)
				s.sendResponse(tx, req, 400) // Bad Request
				return
			}
		}
	}

	// If no SDP body, this is a session refresh (RFC 4028) — just respond with our SDP
	if len(req.Body()) == 0 {
		s.logger.Debugw("re-INVITE with no SDP body (session refresh)", "call_id", callID)
		s.respondWithCurrentSDP(tx, req, session)
		return
	}

	s.logger.Debugw("re-INVITE SDP body (raw)",
		"call_id", callID,
		"sdp_body", string(req.Body()))

	// Parse updated SDP from re-INVITE
	sdpInfo, err := s.ParseSDP(req.Body())
	if err != nil {
		s.logger.Warnw("Failed to parse re-INVITE SDP", "error", err, "call_id", callID)
		s.sendResponse(tx, req, 488) // Not Acceptable Here
		return
	}

	s.logger.Debugw("re-INVITE SDP parsed",
		"call_id", callID,
		"sdp_direction", string(sdpInfo.Direction),
		"sdp_ip", sdpInfo.ConnectionIP,
		"sdp_port", sdpInfo.AudioPort,
		"is_hold", sdpInfo.IsHold())

	// Only update remote RTP when SDP indicates active media (not hold).
	// Hold signals:
	//   - 0.0.0.0 connection IP (RFC 3264 §8.4) — used by Asterisk, FreeSWITCH
	//   - sendonly / inactive direction — used by Twilio, Telnyx, Vonage
	// During hold we keep the previous remote RTP address so audio resumes correctly.
	if !sdpInfo.IsHold() {
		rtpHandler := session.GetRTPHandler()
		if rtpHandler != nil && sdpInfo.ConnectionIP != "" && sdpInfo.AudioPort > 0 {
			rtpHandler.SetRemoteAddr(sdpInfo.ConnectionIP, sdpInfo.AudioPort)
			session.SetRemoteRTP(sdpInfo.ConnectionIP, sdpInfo.AudioPort)
			s.logger.Debugw("Updated remote RTP from re-INVITE",
				"call_id", callID,
				"remote_rtp_ip", sdpInfo.ConnectionIP,
				"remote_rtp_port", sdpInfo.AudioPort)
		}

		// Update codec if the re-INVITE proposes a different one.
		// Asterisk commonly sends re-INVITE after bridging to switch codecs
		// (e.g., direct_media or codec transcoding changes). If we ignore this
		// and keep sending the old payload type, Asterisk sees a PT mismatch
		// and tears down the call immediately.
		if sdpInfo.PreferredCodec != nil {
			currentCodec := session.GetNegotiatedCodec()
			if currentCodec == nil || currentCodec.PayloadType != sdpInfo.PreferredCodec.PayloadType {
				rtpHandler := session.GetRTPHandler()
				if rtpHandler != nil {
					rtpHandler.SetCodec(sdpInfo.PreferredCodec)
				}
				session.SetNegotiatedCodec(sdpInfo.PreferredCodec.Name, int(sdpInfo.PreferredCodec.ClockRate))
				s.logger.Infow("Codec updated from re-INVITE",
					"call_id", callID,
					"new_codec", sdpInfo.PreferredCodec.Name,
					"payload_type", sdpInfo.PreferredCodec.PayloadType)
			}
		}
	} else {
		s.logger.Infow("re-INVITE indicates hold — keeping current RTP target",
			"call_id", callID,
			"sdp_direction", string(sdpInfo.Direction),
			"sdp_ip", sdpInfo.ConnectionIP)
	}

	// Always respond with our SDP (sendrecv) to signal we're ready for media.
	// respondWithCurrentSDP uses the session's negotiated codec, so after any
	// codec switch above, the response will advertise only the correct codec.
	s.respondWithCurrentSDP(tx, req, session)
	s.logger.Infow("re-INVITE handled", "call_id", callID)
}

// respondWithCurrentSDP builds a 200 OK response with the session's current local SDP.
// Used by re-INVITE and UPDATE handlers.
// IMPORTANT: Uses the session's negotiated codec (not all supported codecs) so the
// remote side sees a confirmation of the agreed codec, not a new offer. Advertising
// multiple codecs in a re-INVITE answer confuses Asterisk/FreeSWITCH and can cause
// immediate call teardown ("remote codecs: None" in the peer's logs).
func (s *Server) respondWithCurrentSDP(tx sip.ServerTransaction, req *sip.Request, session *Session) {
	localIP, localPort := session.GetLocalRTP()
	if localIP == "" {
		localIP = s.listenConfig.GetExternalIP()
	}
	codec := session.GetNegotiatedCodec()
	sdpConfig := s.NegotiatedSDPConfig(localIP, localPort, codec)
	sdpBody := s.GenerateSDP(sdpConfig)
	s.sendResponseWithSDPBody(tx, req, sdpBody)
}

func (s *Server) handleAck(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()

	s.mu.RLock()
	session, exists := s.sessions[callID]
	s.mu.RUnlock()

	if !exists {
		s.logger.Warnw("ACK received for unknown session", "call_id", callID)
		return
	}

	// For inbound calls with a dialog session, ReadAck confirms the dialog.
	// NOTE: When RespondSDP is used (which blocks until ACK), the dialog is
	// already confirmed by the time this handler fires. ReadAck is still called
	// for consistency — it's a no-op if CSeq matches.
	if ds := session.GetDialogServerSession(); ds != nil {
		if err := ds.ReadAck(req, tx); err != nil {
			s.logger.Warnw("Dialog ReadAck failed", "error", err, "call_id", callID)
		}
	}

	// Only promote to connected for inbound calls. For outbound calls,
	// handleOutboundDialog owns the state machine via WaitAnswer. ACK is also
	// sent for non-2xx final responses (e.g. 407), and promoting to connected
	// on those causes a spurious initializing→connected→failed transition.
	if session.GetInfo().Direction == CallDirectionInbound {
		session.SetState(CallStateConnected)
		s.logger.Debugw("SIP call established (ACK received)", "call_id", callID)
	}
}

func (s *Server) handleBye(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	fromHdr := req.From()
	fromUser := ""
	if fromHdr != nil {
		fromUser = fromHdr.Address.User
	}

	s.mu.RLock()
	session, exists := s.sessions[callID]
	s.mu.RUnlock()

	if !exists {
		// Try the outbound dialog cache — maybe this BYE is for a dialog we created
		// but haven't registered in sessions yet, or that was already cleaned up.
		if err := s.dialogClientCache.ReadBye(req, tx); err == nil {
			s.logger.Infow("BYE handled by dialog client cache (no session)", "call_id", callID)
			return
		}
		s.logger.Warnw("BYE received for unknown session", "call_id", callID, "from", fromUser)
		s.sendResponse(tx, req, 481) // Call/Transaction Does Not Exist
		return
	}

	info := session.GetInfo()
	connectedDuration := ""
	if info.ConnectedTime != nil {
		connectedDuration = time.Since(*info.ConnectedTime).String()
	}
	s.logger.Infow("BYE received — tearing down call",
		"call_id", callID,
		"from", fromUser,
		"direction", info.Direction,
		"state", info.State,
		"duration", info.Duration,
		"connected_duration", connectedDuration,
		"session_ended", session.IsEnded())

	// For outbound calls, let the DialogClientCache handle the BYE.
	// ReadBye sends 200 OK, sets dialog state to Ended, and cancels the dialog's
	// context — which unblocks handleOutboundDialog's select{} loop.
	//
	// IMPORTANT: For outbound calls, we do NOT call session.End() or removeSession()
	// here. The handleOutboundDialog goroutine owns the session lifecycle. It calls
	// onInvite synchronously (which launches startCall), then waits on the dialog
	// context. If we called session.End() here, it would kill the session while
	// onInvite/startCall is still setting up, causing "Session already ended before
	// startCall". Instead, handleOutboundDialog will call session.End() after
	// onInvite returns and the select{} fires.
	if info.Direction == CallDirectionOutbound {
		// Notify the session that BYE was received BEFORE processing the dialog.
		// This allows startCall (which may still be initializing) to detect the BYE
		// via session.ByeReceived() and shut down gracefully — without relying on
		// session.End() which would prematurely kill RTP and channels.
		session.NotifyBye()

		if err := s.dialogClientCache.ReadBye(req, tx); err != nil {
			// If dialog cache can't handle it (dialog already gone), respond ourselves
			s.logger.Warnw("Dialog cache ReadBye failed, responding directly",
				"error", err, "call_id", callID)
			s.sendResponse(tx, req, 200)
		}
		s.logger.Infow("Outbound BYE processed via dialog cache — session lifecycle delegated to handleOutboundDialog",
			"call_id", callID,
			"duration", info.Duration)

		// Fire the onBye callback for application-level cleanup
		s.mu.RLock()
		onBye := s.onBye
		s.mu.RUnlock()
		if onBye != nil {
			if err := onBye(session); err != nil {
				s.logger.Warnw("BYE handler returned error", "error", err, "call_id", callID)
			}
		}
		return
	}

	// Inbound call — respond 200 OK and tear down.
	// Use the dialog server cache if available (handles To-tag matching and
	// sets dialog state to Ended). Fall back to manual 200 OK otherwise.
	if err := s.dialogServerCache.ReadBye(req, tx); err != nil {
		s.logger.Debugw("Dialog server cache ReadBye failed, responding directly",
			"error", err, "call_id", callID)
		s.sendResponse(tx, req, 200)
	}

	// Remove session (also releases RTP port).
	s.removeSession(callID)

	// Get callback before calling it
	s.mu.RLock()
	onBye := s.onBye
	s.mu.RUnlock()

	if onBye != nil {
		if err := onBye(session); err != nil {
			s.logger.Warnw("BYE handler returned error", "error", err, "call_id", callID)
		}
	}

	// Remote sent BYE — clear onDisconnect so session.End() does NOT send
	// BYE back. The remote already knows the call is over.
	session.ClearOnDisconnect()
	session.End()
	s.logger.Infow("SIP call ended (BYE processed)", "call_id", callID, "duration", info.Duration)
}

func (s *Server) handleCancel(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()

	s.mu.RLock()
	session, exists := s.sessions[callID]
	s.mu.RUnlock()

	// Remove session (also releases RTP port)
	s.removeSession(callID)

	if !exists {
		s.logger.Warnw("CANCEL received for unknown session", "call_id", callID)
		s.sendResponse(tx, req, 481) // Call/Transaction Does Not Exist
		return
	}

	// Get callback before calling it
	s.mu.RLock()
	onCancel := s.onCancel
	s.mu.RUnlock()

	if onCancel != nil {
		if err := onCancel(session); err != nil {
			s.logger.Warnw("CANCEL handler returned error", "error", err, "call_id", callID)
		}
	}

	// CANCEL is for an unanswered INVITE — clear onDisconnect so End()
	// does not attempt to send BYE (no dialog established yet).
	session.ClearOnDisconnect()
	session.End()
	s.sendResponse(tx, req, 200) // OK
	s.logger.Infow("SIP call cancelled", "call_id", callID)
}

func (s *Server) handleRegister(req *sip.Request, tx sip.ServerTransaction) {
	s.logger.Debugw("REGISTER received")
	s.sendResponse(tx, req, 200) // OK
}

func (s *Server) handleOptions(req *sip.Request, tx sip.ServerTransaction) {
	s.logger.Debugw("OPTIONS received")
	s.sendResponse(tx, req, 200) // OK
}

// handleUpdate processes SIP UPDATE requests (RFC 3311).
// Used by various providers for:
//   - Asterisk/FreeSWITCH: direct_media negotiation, session timers, codec changes
//   - Twilio/Telnyx: early media SDP updates, session parameter changes
//   - Vonage: codec renegotiation during call setup
//
// For in-dialog UPDATEs with SDP: update remote RTP (unless hold), respond with our SDP.
// For UPDATEs without SDP or unknown sessions: accept gracefully to keep dialog alive.
func (s *Server) handleUpdate(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	fromUser := ""
	if fromHdr := req.From(); fromHdr != nil {
		fromUser = fromHdr.Address.User
	}

	s.logger.Infow("UPDATE received",
		"call_id", callID,
		"from", fromUser)

	s.mu.RLock()
	session, exists := s.sessions[callID]
	s.mu.RUnlock()

	if !exists || session == nil {
		s.logger.Debugw("UPDATE for unknown session, accepting", "call_id", callID)
		s.sendResponse(tx, req, 200)
		return
	}

	// If SDP body present, handle media renegotiation with hold detection
	if body := req.Body(); len(body) > 0 {
		sdpInfo, err := s.ParseSDP(body)
		if err != nil {
			s.logger.Warnw("Failed to parse UPDATE SDP", "error", err, "call_id", callID)
			s.sendResponse(tx, req, 200) // Accept anyway to keep dialog alive
			return
		}

		s.logger.Debugw("UPDATE SDP parsed",
			"call_id", callID,
			"sdp_direction", string(sdpInfo.Direction),
			"sdp_ip", sdpInfo.ConnectionIP,
			"sdp_port", sdpInfo.AudioPort,
			"is_hold", sdpInfo.IsHold())

		// Only update remote RTP for active media (not hold)
		if !sdpInfo.IsHold() {
			rtpHandler := session.GetRTPHandler()
			if rtpHandler != nil && sdpInfo.ConnectionIP != "" && sdpInfo.AudioPort > 0 {
				rtpHandler.SetRemoteAddr(sdpInfo.ConnectionIP, sdpInfo.AudioPort)
				session.SetRemoteRTP(sdpInfo.ConnectionIP, sdpInfo.AudioPort)
				s.logger.Debugw("Updated remote RTP from UPDATE",
					"call_id", callID,
					"remote_rtp_ip", sdpInfo.ConnectionIP,
					"remote_rtp_port", sdpInfo.AudioPort)
			}

			// Update codec if UPDATE proposes a different one
			if sdpInfo.PreferredCodec != nil {
				currentCodec := session.GetNegotiatedCodec()
				if currentCodec == nil || currentCodec.PayloadType != sdpInfo.PreferredCodec.PayloadType {
					rtpHandler := session.GetRTPHandler()
					if rtpHandler != nil {
						rtpHandler.SetCodec(sdpInfo.PreferredCodec)
					}
					session.SetNegotiatedCodec(sdpInfo.PreferredCodec.Name, int(sdpInfo.PreferredCodec.ClockRate))
					s.logger.Infow("Codec updated from UPDATE",
						"call_id", callID,
						"new_codec", sdpInfo.PreferredCodec.Name,
						"payload_type", sdpInfo.PreferredCodec.PayloadType)
				}
			}
		} else {
			s.logger.Infow("UPDATE indicates hold — keeping current RTP target",
				"call_id", callID,
				"sdp_direction", string(sdpInfo.Direction),
				"sdp_ip", sdpInfo.ConnectionIP)
		}

		s.respondWithCurrentSDP(tx, req, session)
	} else {
		s.sendResponse(tx, req, 200)
	}

	s.logger.Debugw("UPDATE handled", "call_id", callID)
}

// handleInfo processes SIP INFO requests (RFC 6086).
// Used by providers for:
//   - Asterisk/FreeSWITCH: DTMF relay (application/dtmf-relay), call recording
//   - Twilio: session metadata, custom headers
//   - Generic: application/ooh323 info, broadsoft call center events
func (s *Server) handleInfo(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	contentType := ""
	if ct := req.GetHeader("Content-Type"); ct != nil {
		contentType = ct.Value()
	}
	s.logger.Debugw("INFO received",
		"call_id", callID,
		"content_type", contentType)
	s.sendResponse(tx, req, 200)
}

// handleNotify processes SIP NOTIFY requests (RFC 6665).
// Used by providers for:
//   - Twilio/Telnyx: REFER progress (sipfrag), subscription state updates
//   - Asterisk: MWI (message-summary), dialog-info, presence
//   - Vonage: session progress events
func (s *Server) handleNotify(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	eventHdr := ""
	if ev := req.GetHeader("Event"); ev != nil {
		eventHdr = ev.Value()
	}
	s.logger.Debugw("NOTIFY received",
		"call_id", callID,
		"event", eventHdr)
	s.sendResponse(tx, req, 200)
}

// handleRefer processes SIP REFER requests (RFC 3515).
// Inbound REFER (provider-initiated transfer) is declined. The platform supports
// transfer via B2BUA bridge (INVITE-based), triggered by the LLM tool — not REFER.
func (s *Server) handleRefer(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	referTo := ""
	if rt := req.GetHeader("Refer-To"); rt != nil {
		referTo = rt.Value()
	}
	s.logger.Warnw("REFER received (call transfer not supported)",
		"call_id", callID,
		"refer_to", referTo)
	s.sendResponse(tx, req, 603) // Decline
}

// handleSubscribe processes SIP SUBSCRIBE requests (RFC 6665).
// Twilio and some SIP trunks send SUBSCRIBE for dialog-info, presence, or MWI.
// We don't support event subscriptions, so respond with 489 Bad Event to
// signal this cleanly. Using 489 instead of 405/603 prevents Twilio from
// retrying the subscription endlessly.
func (s *Server) handleSubscribe(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	eventHdr := ""
	if ev := req.GetHeader("Event"); ev != nil {
		eventHdr = ev.Value()
	}
	s.logger.Debugw("SUBSCRIBE received (event subscriptions not supported)",
		"call_id", callID,
		"event", eventHdr)
	resp := sip.NewResponseFromRequest(req, 489, "Bad Event", nil)
	if err := tx.Respond(resp); err != nil {
		s.logger.Errorw("Failed to send 489 for SUBSCRIBE", "error", err, "call_id", callID)
	}
}

// handleMessage processes SIP MESSAGE requests (RFC 3428).
// Used by FreeSWITCH for text events and by some SIP providers for out-of-band data.
func (s *Server) handleMessage(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	s.logger.Debugw("MESSAGE received", "call_id", callID)
	s.sendResponse(tx, req, 200)
}

// handleUnknownRequest is the catch-all handler for SIP methods without an explicit handler.
// This is critical for provider compatibility:
//   - Asterisk may send PUBLISH, MESSAGE, SUBSCRIBE in certain configurations
//   - Twilio sends SUBSCRIBE for dialog-info events
//   - FreeSWITCH sends MESSAGE for T.38 fax negotiation
//   - Vonage may send PRACK for reliable provisional responses
//
// For in-dialog requests (known Call-ID): respond 200 OK to prevent dialog teardown.
// For out-of-dialog SUBSCRIBE: respond 489 Bad Event (no event package supported).
// For other out-of-dialog requests: respond 405 Method Not Allowed.
func (s *Server) handleUnknownRequest(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	method := string(req.Method)
	fromUser := ""
	if fromHdr := req.From(); fromHdr != nil {
		fromUser = fromHdr.Address.User
	}

	s.mu.RLock()
	_, inDialog := s.sessions[callID]
	s.mu.RUnlock()

	if inDialog {
		// In-dialog: accept unknown methods to keep the dialog alive.
		// Rejecting with 405 causes Asterisk/FreeSWITCH/Twilio to tear down the call.
		s.logger.Warnw("Unhandled SIP method for active session — accepting to keep dialog alive",
			"method", method,
			"call_id", callID,
			"from", fromUser)
		s.sendResponse(tx, req, 200)
	} else {
		// Out-of-dialog: use RFC-appropriate rejection codes.
		// SUBSCRIBE without a matching event package → 489 Bad Event
		// to prevent subscription loops (Twilio retries on 405).
		if req.Method == sip.SUBSCRIBE {
			s.logger.Debugw("Out-of-dialog SUBSCRIBE rejected",
				"call_id", callID,
				"from", fromUser)
			resp := sip.NewResponseFromRequest(req, 489, "Bad Event", nil)
			if err := tx.Respond(resp); err != nil {
				s.logger.Errorw("Failed to send 489 response", "error", err)
			}
		} else {
			s.logger.Warnw("Unknown SIP method received (no session) — rejecting",
				"method", method,
				"call_id", callID,
				"from", fromUser)
			s.sendResponse(tx, req, 405) // Method Not Allowed
		}
	}
}

func (s *Server) sendResponse(tx sip.ServerTransaction, req *sip.Request, statusCode int) {
	resp := sip.NewResponseFromRequest(req, statusCode, "", nil)
	if err := tx.Respond(resp); err != nil {
		s.logger.Errorw("Failed to send SIP response",
			"error", err,
			"status", statusCode,
			"call_id", req.CallID().Value())
	}
}

// sendResponseWithSDPBody sends a SIP 200 OK response with the given SDP body.
// Adds a Contact header (required by RFC 3261 §13.3.1.1 for INVITE/re-INVITE responses)
// so that Asterisk, Twilio, and other providers know where to send subsequent requests.
func (s *Server) sendResponseWithSDPBody(tx sip.ServerTransaction, req *sip.Request, sdpBody string) {
	s.logger.Debugw("Sending SIP response with SDP",
		"call_id", req.CallID().Value(),
		"method", req.Method,
		"sdp_body", sdpBody)
	resp := sip.NewSDPResponseFromRequest(req, []byte(sdpBody))

	// Add Contact header if not already present — mandatory for INVITE/re-INVITE 200 OK.
	// Without this, Asterisk and other providers cannot route subsequent in-dialog requests
	// (re-INVITEs, BYEs) back to us, causing immediate call teardown.
	if resp.Contact() == nil {
		externalIP := s.listenConfig.GetExternalIP()
		scheme := "sip"
		contactHdr := &sip.ContactHeader{
			Address: sip.Uri{
				Scheme: scheme,
				Host:   externalIP,
				Port:   s.listenConfig.Port,
			},
		}
		resp.AppendHeader(contactHdr)
	}

	if err := tx.Respond(resp); err != nil {
		s.logger.Errorw("Failed to send SIP response with SDP",
			"error", err,
			"call_id", req.CallID().Value())
	}
}

// GetSession returns a session by call ID
func (s *Server) GetSession(callID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, exists := s.sessions[callID]
	return session, exists
}

// EndCall ends a specific call by sending a SIP BYE to the remote party before
// performing local cleanup. Works for both inbound and outbound calls:
//   - Outbound: sends BYE via DialogClientSession (UAC dialog)
//   - Inbound:  sends BYE via DialogServerSession (UAS dialog)
//
// This ensures the remote PBX/provider properly tears down the call leg
// (e.g., Asterisk removes from bridge and frees channel).
// EndCall terminates a call. Delegates to session.End() which owns all teardown:
// BYE (via onDisconnect), RTP stop, context cancel, state transition.
func (s *Server) EndCall(session *Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	session.End()
	return nil
}

// sendBye sends SIP BYE to the remote party via the appropriate dialog session.
// Called by the onDisconnect callback during session.End().
func (s *Server) sendBye(session *Session) {
	callID := session.GetCallID()

	if ds := session.GetDialogClientSession(); ds != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ds.Bye(ctx); err != nil {
			s.logger.Warnw("Failed to send BYE for outbound call",
				"call_id", callID, "error", err)
		} else {
			s.logger.Infow("Sent BYE for outbound call", "call_id", callID)
		}
	}

	if ds := session.GetDialogServerSession(); ds != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ds.Bye(ctx); err != nil {
			s.logger.Warnw("Failed to send BYE for inbound call",
				"call_id", callID, "error", err)
		} else {
			s.logger.Infow("Sent BYE for inbound call", "call_id", callID)
		}
	}
}

// MakeCallOptions holds the typed context for an outbound call.
type MakeCallOptions struct {
	Auth            types.SimplePrinciple
	Assistant       *internal_assistant_entity.Assistant
	ConversationID  uint64
	VaultCredential *protos.VaultCredential
}

// MakeCall initiates an outbound SIP call using the DialogClientCache.
// The cache stores the dialog so incoming BYE/re-INVITE are properly routed
// to the correct DialogClientSession via handleBye → dialogClientCache.ReadBye.
func (s *Server) MakeCall(ctx context.Context, cfg *Config, toURI, fromURI string, opts MakeCallOptions) (*Session, error) {
	if s.state.Load() != int32(ServerStateRunning) {
		return nil, fmt.Errorf("SIP server is not running")
	}

	invite, err := s.prepareOutboundInvite(ctx, cfg, toURI, fromURI)
	if err != nil {
		return nil, err
	}

	session, err := NewSession(ctx, &SessionConfig{
		Config:          cfg,
		Direction:       CallDirectionOutbound,
		CallID:          invite.callID,
		Codec:           &CodecPCMU,
		Logger:          s.logger,
		Auth:            opts.Auth,
		Assistant:       opts.Assistant,
		ConversationID:  opts.ConversationID,
		VaultCredential: opts.VaultCredential,
	})
	if err != nil {
		invite.cleanup()
		return nil, fmt.Errorf("failed to create outbound session: %w", err)
	}

	session.SetLocalRTP(invite.externalIP, invite.localPort)
	session.SetRTPHandler(invite.rtpHandler)
	session.SetDialogClientSession(invite.dialogSession)

	s.registerSession(session, invite.callID)

	// Handle the call lifecycle in background
	go s.handleOutboundDialog(session, invite.rtpHandler, invite.dialogSession)

	return session, nil
}

// handleOutboundDialog processes the outbound dialog lifecycle
func (s *Server) handleOutboundDialog(session *Session, rtpHandler *RTPHandler, dialogSession *sipgo.DialogClientSession) {
	callID := session.GetCallID()

	// Ensure dialog resources are cleaned up when the goroutine exits.
	// sipgo's Close() does NOT send BYE — it only releases internal dialog state.
	defer dialogSession.Close()

	// Wait for the remote side to answer (processes 1xx and 2xx responses).
	// Pass SIP credentials so sipgo can handle digest auth challenges automatically.
	// sipgo's WaitAnswer handles both:
	//   - 401 WWW-Authenticate (Asterisk, Vonage) → responds with Authorization header
	//   - 407 Proxy-Authenticate (Twilio, Telnyx, FreeSWITCH) → responds with Proxy-Authorization
	//   - Only attempts auth once; if second response is still 401/407, returns ErrDialogResponse
	// Mask password for logging (show first char + length)
	maskedPwd := "empty"
	if len(session.config.Password) > 0 {
		maskedPwd = string(session.config.Password[0]) + strings.Repeat("*", len(session.config.Password)-1)
	}
	digestURI := dialogSession.InviteRequest.Recipient.Addr()
	s.logger.Debugw("Outbound call waiting for answer with digest auth",
		"call_id", callID,
		"auth_username", session.config.Username,
		"auth_password", maskedPwd,
		"auth_realm", session.config.Realm,
		"digest_uri", digestURI,
		"request_uri", dialogSession.InviteRequest.Recipient.String())
	err := dialogSession.WaitAnswer(session.ctx, sipgo.AnswerOptions{
		Username: session.config.Username,
		Password: session.config.Password,
		OnResponse: func(res *sip.Response) error {
			statusCode := res.StatusCode
			s.logger.Debugw("Outbound call response",
				"call_id", callID,
				"status", statusCode)

			if statusCode == 180 || statusCode == 183 {
				session.SetState(CallStateRinging)
			}

			// Log digest auth challenge details for debugging credential issues
			if statusCode == 401 {
				if wwwAuth := res.GetHeader("WWW-Authenticate"); wwwAuth != nil {
					s.logger.Debugw("SIP 401 challenge received",
						"call_id", callID,
						"www_authenticate", wwwAuth.Value(),
						"auth_username", session.config.Username)
				}
				if authHdr := dialogSession.InviteRequest.GetHeader("Authorization"); authHdr != nil {
					s.logger.Debugw("SIP digest Authorization sent",
						"call_id", callID,
						"has_authorization", true)
				}
			}
			if statusCode == 407 {
				if proxyAuth := res.GetHeader("Proxy-Authenticate"); proxyAuth != nil {
					s.logger.Debugw("SIP 407 challenge received",
						"call_id", callID,
						"proxy_authenticate", proxyAuth.Value(),
						"auth_username", session.config.Username)
				}
				if authHdr := dialogSession.InviteRequest.GetHeader("Proxy-Authorization"); authHdr != nil {
					s.logger.Debugw("SIP digest Proxy-Authorization sent",
						"call_id", callID,
						"has_proxy_authorization", true)
				}
			}

			return nil
		},
	})
	if err != nil {
		// Extract SIP status code from ErrDialogResponse if available
		var dialogErr *sipgo.ErrDialogResponse
		if errors.As(err, &dialogErr) {
			// If 401/407 after auth attempt, it means credentials are wrong
			if dialogErr.Res.StatusCode == 401 || dialogErr.Res.StatusCode == 407 {
				s.logger.Errorw("Outbound call authentication failed — check SIP credentials in vault",
					"call_id", callID,
					"status", dialogErr.Res.StatusCode,
					"reason", dialogErr.Res.Reason,
					"auth_username", session.config.Username,
					"auth_password_set", len(session.config.Password) > 0,
					"digest_uri", dialogSession.InviteRequest.Recipient.Addr(),
					"hint", "Verify sip_username and sip_password in vault match the SIP provider's auth credentials")
			} else {
				s.logger.Warnw("Outbound call rejected by remote",
					"call_id", callID,
					"status", dialogErr.Res.StatusCode,
					"reason", dialogErr.Res.Reason)
			}
		} else {
			s.logger.Warnw("Outbound call failed",
				"call_id", callID,
				"error", err)
		}
		session.SetState(CallStateFailed)
		s.notifyError(session, err)
		s.removeSession(callID)
		rtpHandler.Stop()
		// Call was never answered — no established dialog, so don't send BYE
		session.ClearOnDisconnect()
		session.End()
		// Allow the transaction layer time to send ACK for non-2xx responses
		// before terminating the dialog (prevents retransmission floods)
		time.AfterFunc(2*time.Second, func() {
			dialogSession.Close()
		})
		return
	}

	// Call answered — 200 OK received.
	// CRITICAL SEQUENCE: Parse SDP → Start RTP → Send ACK
	// Asterisk expects RTP immediately after the call is established. If we
	// send ACK first and then set up RTP, there's a window where Asterisk sees
	// zero RTP packets and tears down the call. By parsing the 200 OK's SDP
	// and firing the first RTP packet BEFORE the ACK, we guarantee Asterisk
	// sees media the instant the dialog is confirmed.
	answerTime := time.Now()
	s.logger.Infow("Outbound call 200 OK received — setting up RTP before ACK",
		"call_id", callID)

	// Step 1: Parse remote SDP from 200 OK (available before ACK)
	// This is where we discover what codec the remote side actually accepted.
	// The initial INVITE offered all SupportedCodecs, but the 200 OK's SDP
	// tells us which one was chosen. We MUST update the RTP handler's codec
	// so outgoing packets use the correct payload type, and update the session
	// so subsequent re-INVITE responses advertise only the negotiated codec.
	var remoteRTPIP string
	var remoteRTPPort int
	if dialogSession.InviteResponse != nil {
		if body := dialogSession.InviteResponse.Body(); len(body) > 0 {
			s.logger.Debugw("Outbound call 200 OK SDP answer (raw)",
				"call_id", callID,
				"sdp_body", string(body))
			sdpInfo, parseErr := s.ParseSDP(body)
			if parseErr == nil && sdpInfo.ConnectionIP != "" && sdpInfo.AudioPort > 0 {
				remoteRTPIP = sdpInfo.ConnectionIP
				remoteRTPPort = sdpInfo.AudioPort
				rtpHandler.SetRemoteAddr(remoteRTPIP, remoteRTPPort)
				session.SetRemoteRTP(remoteRTPIP, remoteRTPPort)

				// Negotiate codec from the answer SDP — the remote side may have
				// chosen PCMA (PT 8) even though we offered PCMU first. If we
				// keep sending PT 0 (PCMU) while the remote expects PT 8 (PCMA),
				// the audio is garbled or the PBX drops the call immediately.
				if sdpInfo.PreferredCodec != nil {
					rtpHandler.SetCodec(sdpInfo.PreferredCodec)
					session.SetNegotiatedCodec(sdpInfo.PreferredCodec.Name, int(sdpInfo.PreferredCodec.ClockRate))
					s.logger.Infow("Outbound call codec negotiated from 200 OK",
						"call_id", callID,
						"codec", sdpInfo.PreferredCodec.Name,
						"payload_type", sdpInfo.PreferredCodec.PayloadType,
						"clock_rate", sdpInfo.PreferredCodec.ClockRate)
				} else {
					s.logger.Warnw("No matching codec in 200 OK SDP, keeping PCMU default",
						"call_id", callID,
						"remote_payload_types", sdpInfo.PayloadTypes)
				}
			} else if parseErr != nil {
				s.logger.Warnw("Failed to parse remote SDP from 200 OK",
					"call_id", callID,
					"error", parseErr)
			}
		} else {
			s.logger.Warnw("No SDP body in 200 OK response", "call_id", callID)
		}
	} else {
		s.logger.Warnw("No InviteResponse available after WaitAnswer", "call_id", callID)
	}

	// Step 2: Start RTP — sends the first silence packet synchronously, then
	// launches sendLoop. This fires BEFORE ACK so Asterisk sees media immediately.
	rtpHandler.Start()

	localIP, localPort := rtpHandler.LocalAddr()
	remoteAddr := rtpHandler.GetRemoteAddr()
	s.logger.Infow("RTP started (pre-ACK)",
		"call_id", callID,
		"local_rtp", fmt.Sprintf("%s:%d", localIP, localPort),
		"remote_rtp", fmt.Sprintf("%s:%d", remoteRTPIP, remoteRTPPort),
		"remote_addr_set", remoteAddr != nil,
		"elapsed_since_200ok_ms", time.Since(answerTime).Milliseconds())

	// Step 3: NOW send ACK — dialog is confirmed, RTP is already flowing.
	if err := dialogSession.Ack(session.ctx); err != nil {
		s.logger.Errorw("Failed to send ACK", "error", err, "call_id", callID)
		session.SetState(CallStateFailed)
		s.removeSession(callID)
		rtpHandler.Stop()
		session.End()
		dialogSession.Close()
		return
	}
	s.logger.Infow("ACK sent (RTP already flowing)",
		"call_id", callID,
		"elapsed_since_200ok_ms", time.Since(answerTime).Milliseconds())

	session.SetState(CallStateConnected)

	// Notify invite handler (which starts the conversation — may do DB lookups).
	// RTP silence is already flowing, so Asterisk won't time out during this.
	s.mu.RLock()
	onInvite := s.onInvite
	s.mu.RUnlock()
	if onInvite != nil {
		info := session.GetInfo()
		s.logger.Infow("Starting onInvite handler for outbound call",
			"call_id", callID)
		if err := onInvite(session, info.LocalURI, info.RemoteURI); err != nil {
			s.logger.Errorw("Outbound INVITE handler failed", "error", err, "call_id", callID)
		} else {
			s.logger.Infow("onInvite handler completed",
				"call_id", callID,
				"total_elapsed_ms", time.Since(answerTime).Milliseconds())
		}
	}

	// Wait for the session to end. The session lifecycle is now owned by startCall
	// (launched by onInvite as a synchronous or goroutine call). startCall blocks on
	// talker.Talk() for the call duration. When BYE arrives:
	//   1. handleBye calls session.NotifyBye() — signals startCall via ByeReceived()
	//   2. handleBye fires onBye → sip.go:handleBye cancels startCall's callCtx
	//   3. talker.Talk returns → startCall finishes → session.End() is called
	//
	// For app-initiated hangup:
	//   EndCall → dialog.Bye + session.End() → session context cancelled
	//
	// We wait on session.Context() because that's cancelled by session.End(), which
	// is the definitive signal that the call is fully torn down. We do NOT call
	// session.End() here when dialog context is cancelled — that was the race condition
	// that killed startCall before it could begin.
	//
	// Safety: if BYE arrives before startCall registers in m.sessions, startCall
	// will detect it via session.ByeReceived() and exit. Either way, session.End()
	// is eventually called, unblocking us here.
	s.logger.Debugw("Outbound dialog waiting for session to end", "call_id", callID)
	select {
	case <-session.Context().Done():
		s.logger.Infow("Outbound dialog ending — session ended",
			"call_id", callID,
			"call_duration_ms", time.Since(answerTime).Milliseconds())
	case <-dialogSession.Context().Done():
		// BYE received — dialog context cancelled. Wait for session to end naturally
		// via startCall's teardown, but apply a safety timeout to prevent leaks.
		s.logger.Infow("Outbound dialog — BYE received, waiting for session teardown",
			"call_id", callID,
			"call_duration_ms", time.Since(answerTime).Milliseconds())
		select {
		case <-session.Context().Done():
			s.logger.Debugw("Outbound dialog — session ended after BYE",
				"call_id", callID)
		case <-time.After(30 * time.Second):
			// Safety: if startCall never started or is stuck, force-end the session
			s.logger.Warnw("Outbound dialog — session did not end within 30s after BYE, forcing teardown",
				"call_id", callID)
			if !session.IsEnded() {
				session.End()
			}
		}
	}

	// Cleanup: stop RTP and remove session from map
	rtpHandler.Stop()
	s.removeSession(callID)
}

// outboundInvite holds the result of prepareOutboundInvite — the allocated
// resources and dialog needed to complete or clean up an outbound call.
type outboundInvite struct {
	rtpHandler    *RTPHandler
	rtpPort       int
	localPort     int
	externalIP    string
	callID        string
	dialogSession *sipgo.DialogClientSession

	server *Server // back-reference for cleanup
}

// cleanup releases all resources allocated during prepareOutboundInvite.
// Safe to call on error paths before the session takes ownership.
func (o *outboundInvite) cleanup() {
	if o.rtpHandler != nil {
		o.rtpHandler.Stop()
	}
	if o.server != nil && o.rtpPort > 0 {
		o.server.rtpAllocator.Release(o.rtpPort)
	}
	if o.dialogSession != nil {
		time.AfterFunc(2*time.Second, func() { o.dialogSession.Close() })
	}
}

// prepareOutboundInvite allocates RTP, builds SDP + INVITE headers, and sends
// the INVITE via the dialog client cache. Returns the allocated resources in an
// outboundInvite struct. The caller must call invite.cleanup() on error, or
// transfer ownership of the resources to a Session on success.
//
// Shared between MakeCall and MakeBridgeCall.
func (s *Server) prepareOutboundInvite(ctx context.Context, cfg *Config, toURI, fromURI string) (*outboundInvite, error) {
	rtpPort, err := s.rtpAllocator.Allocate()
	if err != nil {
		return nil, fmt.Errorf("no RTP ports available: %w", err)
	}

	rtpBindIP := s.listenConfig.GetBindAddress()
	rtpHandler, err := NewRTPHandler(ctx, &RTPConfig{
		LocalIP:     rtpBindIP,
		LocalPort:   rtpPort,
		PayloadType: CodecPCMU.PayloadType,
		ClockRate:   CodecPCMU.ClockRate,
		Logger:      s.logger,
	})
	if err != nil {
		s.rtpAllocator.Release(rtpPort)
		return nil, fmt.Errorf("failed to create RTP handler: %w", err)
	}

	_, localPort := rtpHandler.LocalAddr()
	externalIP := s.listenConfig.GetExternalIP()
	sdpBody := s.GenerateSDP(DefaultSDPConfig(externalIP, localPort))

	scheme := "sip"
	if cfg.Transport == TransportTLS {
		scheme = "sips"
	}

	recipient := sip.Uri{
		Scheme: scheme,
		Host:   cfg.Server,
		Port:   cfg.Port,
		User:   toURI,
	}
	if cfg.Transport == TransportTLS || cfg.Transport == TransportTCP {
		if recipient.UriParams == nil {
			recipient.UriParams = sip.NewParams()
		}
		recipient.UriParams.Add("transport", string(cfg.Transport))
	}

	fromDomain := cfg.Domain
	if fromDomain == "" {
		fromDomain = cfg.Server
	}
	fromUser := strings.TrimSpace(fromURI)
	if cfg.CallerID != "" {
		fromUser = cfg.CallerID
	}
	if fromUser == "" {
		fromUser = cfg.Username
	}
	if fromUser == "" {
		rtpHandler.Stop()
		s.rtpAllocator.Release(rtpPort)
		return nil, fmt.Errorf("SIP From user is empty: fromPhone, caller_id, or sip_username must be set")
	}

	fromHDR := &sip.FromHeader{
		DisplayName: fromURI,
		Address: sip.Uri{
			Scheme: scheme,
			User:   fromUser,
			Host:   fromDomain,
		},
		Params: sip.NewParams(),
	}
	fromHDR.Params.Add("tag", sip.GenerateTagN(16))

	inviteHeaders := []sip.Header{fromHDR}
	if callerID := strings.TrimSpace(fromURI); callerID != "" {
		pai := sip.NewHeader("P-Asserted-Identity", fmt.Sprintf("<%s:%s@%s>", scheme, callerID, fromDomain))
		inviteHeaders = append(inviteHeaders, pai)
	}
	for name, value := range cfg.CustomHeaders {
		inviteHeaders = append(inviteHeaders, sip.NewHeader(name, value))
	}

	dialogSession, err := s.dialogClientCache.Invite(ctx, recipient, []byte(sdpBody), inviteHeaders...)
	if err != nil {
		rtpHandler.Stop()
		s.rtpAllocator.Release(rtpPort)
		return nil, fmt.Errorf("failed to send INVITE: %w", err)
	}

	callID := dialogSession.InviteRequest.CallID().Value()

	return &outboundInvite{
		rtpHandler:    rtpHandler,
		rtpPort:       rtpPort,
		localPort:     localPort,
		externalIP:    externalIP,
		callID:        callID,
		dialogSession: dialogSession,
		server:        s,
	}, nil
}

// answeredCall holds the result of waitForAnswer — the RTP handler with remote
// address set, codec negotiated, RTP started, and ACK sent.
type answeredCall struct {
	rtpHandler      *RTPHandler
	negotiatedCodec *Codec
}

// waitForAnswer blocks until the outbound dialog receives a 200 OK, parses
// the SDP answer, starts RTP, and sends ACK. Returns the answered call state.
//
// Shared between handleOutboundDialog and MakeBridgeCall.
func (s *Server) waitForAnswer(ctx context.Context, invite *outboundInvite, cfg *Config) (*answeredCall, error) {
	err := invite.dialogSession.WaitAnswer(ctx, sipgo.AnswerOptions{
		Username: cfg.Username,
		Password: cfg.Password,
		OnResponse: func(res *sip.Response) error {
			s.logger.Debugw("Outbound call response",
				"call_id", invite.callID, "status", res.StatusCode)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}

	// 200 OK — parse SDP, start RTP before ACK (Asterisk needs media immediately)
	var negotiatedCodec *Codec
	if invite.dialogSession.InviteResponse != nil {
		if body := invite.dialogSession.InviteResponse.Body(); len(body) > 0 {
			sdpInfo, parseErr := s.ParseSDP(body)
			if parseErr == nil && sdpInfo.ConnectionIP != "" && sdpInfo.AudioPort > 0 {
				invite.rtpHandler.SetRemoteAddr(sdpInfo.ConnectionIP, sdpInfo.AudioPort)
				if sdpInfo.PreferredCodec != nil {
					invite.rtpHandler.SetCodec(sdpInfo.PreferredCodec)
					negotiatedCodec = sdpInfo.PreferredCodec
				}
			} else if parseErr != nil {
				s.logger.Warnw("Failed to parse SDP from 200 OK",
					"call_id", invite.callID, "error", parseErr)
			}
		} else {
			s.logger.Warnw("No SDP body in 200 OK response", "call_id", invite.callID)
		}
	} else {
		s.logger.Warnw("No InviteResponse available after WaitAnswer", "call_id", invite.callID)
	}

	invite.rtpHandler.Start()

	ackCtx, ackCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ackCancel()
	if err := invite.dialogSession.Ack(ackCtx); err != nil {
		invite.rtpHandler.Stop()
		return nil, fmt.Errorf("failed to send ACK: %w", err)
	}

	return &answeredCall{
		rtpHandler:      invite.rtpHandler,
		negotiatedCodec: negotiatedCodec,
	}, nil
}

// registerSession registers a session for BYE routing and sets up the
// onDisconnect callback. Used by both MakeCall and MakeBridgeCall.
func (s *Server) registerSession(session *Session, callID string) {
	session.SetOnDisconnect(func(sess *Session) {
		s.sendBye(sess)
		s.removeSession(callID)
	})
	s.mu.Lock()
	s.sessions[callID] = session
	s.sessionCount.Add(1)
	s.mu.Unlock()
}
