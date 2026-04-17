// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package assistant_sip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm/clause"

	"github.com/rapidaai/api/assistant-api/config"
	internal_adapter "github.com/rapidaai/api/assistant-api/internal/adapters"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_telephony "github.com/rapidaai/api/assistant-api/internal/channel/telephony"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	observe_exporters "github.com/rapidaai/api/assistant-api/internal/observe/exporters"
	internal_services "github.com/rapidaai/api/assistant-api/internal/services"
	internal_assistant_service "github.com/rapidaai/api/assistant-api/internal/services/assistant"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
	sip_pipeline "github.com/rapidaai/api/assistant-api/sip/pipeline"
	web_client "github.com/rapidaai/pkg/clients/web"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/connectors"
	gorm_models "github.com/rapidaai/pkg/models/gorm"
	"github.com/rapidaai/pkg/storages"
	storage_files "github.com/rapidaai/pkg/storages/file-storage"
	"github.com/rapidaai/pkg/types"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

// SIPEngine manages a multi-tenant SIP server. Config is resolved per-call
// from each assistant's phone deployment and vault credentials.
type SIPEngine struct {
	cfg    *config.AssistantConfig
	logger commons.Logger
	mu     sync.RWMutex
	server *sip_infra.Server

	ctx    context.Context
	cancel context.CancelFunc

	postgres   connectors.PostgresConnector
	redis      connectors.RedisConnector
	opensearch connectors.OpenSearchConnector
	storage    storages.Storage

	assistantConversationService internal_services.AssistantConversationService
	assistantService             internal_services.AssistantService
	deploymentService            internal_services.AssistantDeploymentService
	vaultClient                  web_client.VaultClient

	// Registration client for maintaining SIP REGISTER with external providers.
	registrationClient *sip_infra.RegistrationClient

	// Pipeline dispatcher — routes SIP call lifecycle through extensible stages.
	dispatcher *sip_pipeline.Dispatcher
}

func NewSIPEngine(config *config.AssistantConfig, logger commons.Logger,
	postgres connectors.PostgresConnector,
	redis connectors.RedisConnector,
	opensearch connectors.OpenSearchConnector,
	vectordb connectors.VectorConnector) *SIPEngine {
	return &SIPEngine{
		cfg:                          config,
		logger:                       logger,
		postgres:                     postgres,
		redis:                        redis,
		opensearch:                   opensearch,
		assistantConversationService: internal_assistant_service.NewAssistantConversationService(logger, postgres, storage_files.NewStorage(config.AssetStoreConfig, logger)),
		assistantService:             internal_assistant_service.NewAssistantService(config, logger, postgres, opensearch),
		deploymentService:            internal_assistant_service.NewAssistantDeploymentService(config, logger, postgres),
		storage:                      storage_files.NewStorage(config.AssetStoreConfig, logger),
		vaultClient:                  web_client.NewVaultClientGRPC(&config.AppConfig, logger, redis),
	}
}

func (m *SIPEngine) listenConfig() *sip_infra.ListenConfig {
	transportType := sip_infra.TransportUDP
	switch m.cfg.SIPConfig.Transport {
	case "tcp":
		transportType = sip_infra.TransportTCP
	case "tls":
		transportType = sip_infra.TransportTLS
	}
	lc := &sip_infra.ListenConfig{
		Address:    m.cfg.SIPConfig.Server,
		ExternalIP: m.cfg.SIPConfig.ExternalIP,
		Port:       m.cfg.SIPConfig.Port,
		Transport:  transportType,
	}
	m.logger.Infow("SIP ListenConfig from app config",
		"address", lc.Address,
		"external_ip", lc.ExternalIP,
		"port", lc.Port,
		"transport", lc.Transport,
		"raw_sip_config_external_ip", m.cfg.SIPConfig.ExternalIP,
		"raw_sip_config_server", m.cfg.SIPConfig.Server)
	return lc
}

// Connect initializes the SIP server. The middleware chain resolves the
// assistant from the DID in the To-URI:
//
//	routingMiddleware (DID lookup) -> assistantMiddleware -> vaultConfigResolver
func (m *SIPEngine) Connect(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	server, err := sip_infra.NewServer(m.ctx, &sip_infra.ServerConfig{
		ListenConfig:      m.listenConfig(),
		Logger:            m.logger,
		RedisClient:       m.redis.GetConnection(),
		RTPPortRangeStart: m.cfg.SIPConfig.RTPPortRangeStart,
		RTPPortRangeEnd:   m.cfg.SIPConfig.RTPPortRangeEnd,
	})
	if err != nil {
		return fmt.Errorf("failed to create SIP server: %w", err)
	}

	server.SetMiddlewares(
		[]sip_infra.Middleware{
			m.routingMiddleware,   // Resolve assistant by DID
			m.assistantMiddleware, // Load assistant entity
		},
		m.vaultConfigResolver, // Fetch SIP config from vault
	)

	server.SetOnInvite(m.onInvite)
	server.SetOnBye(m.onBye)
	server.SetOnCancel(m.onCancel)
	server.SetOnError(m.onError)

	m.registrationClient = sip_infra.NewRegistrationClient(server.Client(), server.GetListenConfig(), m.logger)

	m.dispatcher = sip_pipeline.NewDispatcher(&sip_pipeline.DispatcherConfig{
		Logger:               m.logger,
		Server:               server,
		RegistrationClient:   m.registrationClient,
		DIDResolver:          m.resolveAssistantByDID,
		OnCreateConversation: m.pipelineCreateConversation,
		OnCallSetup:          m.pipelineCallSetup,
		OnCallStart:          m.pipelineCallStart,
		OnCallEnd:            m.pipelineCallEnd,
		OnCreateObserver:     m.createObserver,
	})
	m.dispatcher.Start(m.ctx)

	// Start server AFTER dispatcher is ready — incoming INVITEs call m.dispatcher.OnPipeline
	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start SIP server: %w", err)
	}
	m.server = server

	// Initial registration sync — runs before returning so DIDs are active before calls arrive.
	m.reconcileRegistrations(m.ctx)

	// Background watcher — polls DB every 5 minutes for new/removed/changed deployments.
	go m.startRegistrationWatcher(m.ctx)

	return nil
}

func (m *SIPEngine) GetServer() *sip_infra.Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.server
}

// assistantMiddleware loads the assistant entity and verifies project-level access.
func (m *SIPEngine) assistantMiddleware(ctx *sip_infra.SIPRequestContext, next func() (*sip_infra.InviteResult, error)) (*sip_infra.InviteResult, error) {
	authVal, _ := ctx.Get("auth")
	auth, _ := authVal.(types.SimplePrinciple)
	if auth == nil {
		return sip_infra.Reject(401, "Authentication required"), nil
	}

	if ctx.AssistantID == "" {
		return sip_infra.Reject(404, "Invalid SIP URI format, expected: sip:{assistantID}:{apiKey}@host"), nil
	}
	assistantID, err := strconv.ParseUint(ctx.AssistantID, 10, 64)
	if err != nil {
		m.logger.Warnw("SIP: invalid assistant ID", "call_id", ctx.CallID, "method", ctx.Method, "assistant_id", ctx.AssistantID)
		return sip_infra.Reject(404, "Invalid assistant ID format"), nil
	}

	assistant, err := m.assistantService.Get(m.ctx, auth, assistantID, utils.GetVersionDefinition("latest"),
		&internal_services.GetAssistantOption{InjectPhoneDeployment: true})
	if err != nil {
		m.logger.Error("SIP: assistant not found", "call_id", ctx.CallID, "method", ctx.Method, "assistant_id", assistantID, "error", err)
		return sip_infra.Reject(404, "Assistant not found"), nil
	}

	if !m.hasAccessToAssistant(auth, assistant) {
		return sip_infra.Reject(403, "API key does not have access to this assistant"), nil
	}

	ctx.Set("assistant", assistant)
	return next()
}

// vaultConfigResolver is the terminal middleware handler. It fetches provider
// config from vault and returns the InviteResult with resolved metadata.
func (m *SIPEngine) vaultConfigResolver(ctx *sip_infra.SIPRequestContext) (*sip_infra.InviteResult, error) {
	authVal, _ := ctx.Get("auth")
	auth, _ := authVal.(types.SimplePrinciple)
	assistantVal, _ := ctx.Get("assistant")
	assistant, _ := assistantVal.(*internal_assistant_entity.Assistant)

	if auth == nil || assistant == nil {
		return sip_infra.Reject(500, "Middleware chain incomplete"), nil
	}

	sipConfig, vaultCred, err := m.fetchSIPConfigAndVaultCredential(auth, assistant)
	if err != nil {
		m.logger.Error("SIP: failed to resolve config", "call_id", ctx.CallID, "method", ctx.Method, "error", err)
		return sip_infra.Reject(500, "Failed to resolve SIP configuration"), nil
	}

	var orgID uint64
	if auth.GetCurrentOrganizationId() != nil {
		orgID = *auth.GetCurrentOrganizationId()
	}
	m.logger.Infow("SIP request authenticated",
		"call_id", ctx.CallID,
		"method", ctx.Method,
		"assistant_id", assistant.Id,
		"org_id", orgID)

	return sip_infra.AllowWithExtra(sipConfig, map[string]interface{}{
		"auth":             auth,
		"assistant":        assistant,
		"sip_config":       sipConfig,
		"vault_credential": vaultCred,
	}), nil
}

func (m *SIPEngine) hasAccessToAssistant(auth types.SimplePrinciple, assistant *internal_assistant_entity.Assistant) bool {
	if auth.GetCurrentProjectId() == nil || assistant.ProjectId == 0 {
		return false
	}
	return *auth.GetCurrentProjectId() == assistant.ProjectId
}

// onInvite routes incoming INVITE into the pipeline.
// For inbound: middleware chain sets "auth" and "assistant" on the session.
// For outbound: telephony.OutboundCall sets "auth" + "assistant_id" (uint64) but
// not the full entity, so we resolve it here from the ID.
func (m *SIPEngine) onInvite(session *sip_infra.Session, fromURI, toURI string) error {
	info := session.GetInfo()
	callID := info.CallID

	if session.IsEnded() {
		return fmt.Errorf("session already ended")
	}

	auth := session.GetAuth()
	if auth == nil {
		return fmt.Errorf("missing auth on session %s", callID)
	}

	assistant := session.GetAssistant()
	if assistant == nil {
		return fmt.Errorf("missing assistant context on session %s", callID)
	}

	// For outbound, conversation_id is set on the session by MakeCall.
	// For inbound, it's 0 here — created later by handleSessionEstablished.
	conversationID := session.GetConversationID()

	m.dispatcher.OnPipeline(m.ctx, sip_infra.SessionEstablishedPipeline{
		ID:              callID,
		Session:         session,
		Config:          session.GetConfig(),
		VaultCredential: session.GetVaultCredential(),
		Direction:       info.Direction,
		AssistantID:     assistant.Id,
		Auth:            auth,
		FromURI:         fromURI,
		ConversationID:  conversationID,
	})

	return nil
}

func (m *SIPEngine) onBye(session *sip_infra.Session) error {
	m.dispatcher.OnPipeline(m.ctx, sip_infra.ByeReceivedPipeline{
		ID:      session.GetInfo().CallID,
		Session: session,
	})
	return nil
}

func (m *SIPEngine) onCancel(session *sip_infra.Session) error {
	m.dispatcher.OnPipeline(m.ctx, sip_infra.CancelReceivedPipeline{
		ID:      session.GetInfo().CallID,
		Session: session,
	})
	return nil
}

// onError handles SIP-level errors by emitting a CallFailedPipeline event.
// The pipeline handler (signal.go) creates the observer and persists metrics.
func (m *SIPEngine) onError(session *sip_infra.Session, callErr error) {
	m.logger.Warnw("SIP error", "call_id", session.GetCallID(), "error", callErr)
	m.dispatcher.OnPipeline(m.ctx, sip_infra.CallFailedPipeline{
		ID:      session.GetCallID(),
		Session: session,
		Error:   callErr,
	})
}

func (m *SIPEngine) EndCall(callID string) error {
	m.mu.RLock()
	srv := m.server
	m.mu.RUnlock()
	if srv == nil {
		return fmt.Errorf("SIP server not running")
	}
	session, ok := srv.GetSession(callID)
	if !ok {
		return fmt.Errorf("session %s not found", callID)
	}
	return srv.EndCall(session)
}

func (m *SIPEngine) GetActiveCalls() int {
	m.mu.RLock()
	srv := m.server
	m.mu.RUnlock()
	if srv != nil {
		return srv.SessionCount()
	}
	return 0
}

func (m *SIPEngine) Stop() {
	if m.registrationClient != nil {
		m.registrationClient.UnregisterAll(context.Background())
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Lock()
	srv := m.server
	m.server = nil
	m.mu.Unlock()
	if srv != nil {
		srv.Stop()
	}
	m.logger.Infow("SIP Manager stopped")
}

func (m *SIPEngine) Disconnect(ctx context.Context) error {
	m.Stop()
	return nil
}

func (m *SIPEngine) fetchSIPConfigAndVaultCredential(auth types.SimplePrinciple, assistant *internal_assistant_entity.Assistant) (*sip_infra.Config, *protos.VaultCredential, error) {
	if assistant.AssistantPhoneDeployment == nil {
		return nil, nil, fmt.Errorf("assistant has no phone deployment configured")
	}

	opts := assistant.AssistantPhoneDeployment.GetOptions()
	credentialID, err := opts.GetUint64("rapida.credential_id")
	if err != nil {
		return nil, nil, fmt.Errorf("no credential_id in phone deployment: %w", err)
	}

	vaultCred, err := m.vaultClient.GetCredential(m.ctx, auth, credentialID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch vault credential %d: %w", credentialID, err)
	}

	sipConfig, err := sip_infra.ParseConfigFromVault(vaultCred)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse SIP config from vault: %w", err)
	}

	// Set CallerID to the assistant's DID from the phone deployment.
	// This is used as the From URI user in outbound INVITEs (prepareOutboundInvite).
	if did, err := opts.GetString("phone"); err == nil && did != "" {
		sipConfig.CallerID = strings.TrimPrefix(did, "+")
	}

	if m.cfg.SIPConfig != nil {
		sipConfig.ApplyOperationalDefaults(
			m.cfg.SIPConfig.Port,
			sip_infra.Transport(m.cfg.SIPConfig.Transport),
			m.cfg.SIPConfig.RTPPortRangeStart,
			m.cfg.SIPConfig.RTPPortRangeEnd,
		)
	}

	return sipConfig, vaultCred, nil
}

// routingMiddleware resolves the assistant for an inbound INVITE by looking up
// the DID from the To-URI (or From-URI fallback) against phone deployments.
func (m *SIPEngine) routingMiddleware(ctx *sip_infra.SIPRequestContext, next func() (*sip_infra.InviteResult, error)) (*sip_infra.InviteResult, error) {
	did := sip_infra.ExtractDIDFromURI(ctx.ToURI)
	if did == "" {
		did = sip_infra.ExtractDIDFromURI(ctx.FromURI)
	}
	if did == "" {
		return sip_infra.Reject(404, "No DID found in SIP URI"), nil
	}

	assistantID, auth, err := m.resolveAssistantByDID(did)
	if err != nil {
		m.logger.Warnw("SIP: DID lookup failed",
			"call_id", ctx.CallID,
			"did", did,
			"error", err)
		return sip_infra.Reject(404, "No assistant found for this number"), nil
	}

	ctx.AssistantID = strconv.FormatUint(assistantID, 10)
	ctx.Set("auth", auth)

	m.logger.Infow("SIP: Routed by DID",
		"call_id", ctx.CallID,
		"did", did,
		"assistant_id", assistantID)

	return next()
}

// resolveAssistantByDID looks up which assistant owns the given DID (phone number)
// using a single joined query across assistants, phone deployments, and telephony options.
func (m *SIPEngine) resolveAssistantByDID(did string) (uint64, types.SimplePrinciple, error) {
	db := m.postgres.DB(m.ctx)

	type didLookupResult struct {
		AssistantID    uint64
		ProjectID      uint64
		OrganizationID uint64
	}
	var result didLookupResult
	tx := db.Raw(`
		SELECT a.id as assistant_id, a.project_id, a.organization_id
		FROM assistants a
		JOIN assistant_phone_deployments apd ON apd.assistant_id = a.id
		JOIN assistant_deployment_telephony_options o ON o.assistant_deployment_telephony_id = apd.id
		WHERE apd.telephony_provider = ? AND o.key = ? AND (o.value = ? OR o.value = ?)`,
		"sip", "phone", did, strings.TrimPrefix(did, "+")).
		First(&result)
	if tx.Error != nil {
		return 0, nil, fmt.Errorf("no SIP phone deployment found for DID %s: %w", did, tx.Error)
	}

	projectScope := &types.ProjectScope{
		ProjectId:      &result.ProjectID,
		OrganizationId: &result.OrganizationID,
	}
	return result.AssistantID, projectScope, nil
}

const registrationPollInterval = 5 * time.Minute

// startRegistrationWatcher polls DB every 5 minutes for new/removed SIP phone deployments.
func (m *SIPEngine) startRegistrationWatcher(ctx context.Context) {
	ticker := time.NewTicker(registrationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconcileRegistrations(ctx)
		}
	}
}

// reconcileRegistrations syncs active SIP registrations with DB phone deployments.
// - New deployments → register
// - Removed deployments → unregister
// - Failed/disabled deployments → skip
// - Already active → no-op (renewLoop handles renewal)
func (m *SIPEngine) reconcileRegistrations(ctx context.Context) {
	db := m.postgres.DB(ctx)

	var deployments []internal_assistant_entity.AssistantPhoneDeployment
	tx := db.
		Preload("TelephonyOption").
		Where("telephony_provider = ?", "sip").
		Find(&deployments)
	if tx.Error != nil {
		m.logger.Warnw("Failed to load SIP phone deployments", "error", tx.Error)
		return
	}

	// Build desired state from DB
	type desiredDID struct {
		DID          string
		CredentialID uint64
		AssistantID  uint64
		DeploymentID uint64
		Status       string
	}
	desired := make(map[string]desiredDID)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for _, dep := range deployments {
		opts := dep.GetOptions()
		did, _ := opts.GetString("phone")
		if did == "" {
			continue
		}
		did = sip_infra.NormalizeDID(did)
		credentialID, err := opts.GetUint64("rapida.credential_id")
		if err != nil {
			continue
		}
		sipStatus, _ := opts.GetString("rapida.sip_status")

		// Skip deployments with terminal registration statuses
		switch sipStatus {
		case "failed", "disabled", "rejected", "config_error", "unreachable":
			continue
		}

		// Only register DIDs that opted into inbound calls
		sipInbound, _ := opts.GetString("rapida.sip_inbound")
		if sipInbound != "true" {
			continue
		}

		desired[did] = desiredDID{
			DID:          did,
			CredentialID: credentialID,
			AssistantID:  dep.AssistantId,
			DeploymentID: dep.Id,
			Status:       sipStatus,
		}
	}

	// Current active registrations
	activeDIDs := m.registrationClient.GetRegisteredDIDs()
	activeSet := make(map[string]bool, len(activeDIDs))
	for _, did := range activeDIDs {
		activeSet[did] = true
	}

	// Register new DIDs (in DB but not active)
	for did, d := range desired {
		if activeSet[did] {
			continue
		}
		d := d
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			m.registerDID(ctx, d.DID, d.CredentialID, d.AssistantID, d.DeploymentID)
		}()
	}

	// Unregister removed DIDs (active but not in DB)
	for _, did := range activeDIDs {
		if _, ok := desired[did]; !ok {
			m.logger.Infow("Unregistering removed DID", "did", did)
			if err := m.registrationClient.Unregister(ctx, did); err != nil {
				m.logger.Warnw("Failed to unregister removed DID", "did", did, "error", err)
			}
		}
	}

	wg.Wait()

	if len(desired) > 0 {
		m.logger.Debugw("Registration reconciliation complete",
			"desired", len(desired),
			"active", m.registrationClient.ActiveCount())
	}
}

// registerDID registers a single DID with its SIP provider and writes status back to DB.
func (m *SIPEngine) registerDID(ctx context.Context, did string, credentialID, assistantID, deploymentID uint64) {
	db := m.postgres.DB(ctx)

	var assistant internal_assistant_entity.Assistant
	if err := db.Where("id = ?", assistantID).First(&assistant).Error; err != nil {
		m.logger.Warnw("Failed to load assistant for registration",
			"assistant_id", assistantID, "error", err)
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_status", "config_error")
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_error", "assistant not found")
		return
	}
	auth := &types.ProjectScope{
		ProjectId:      &assistant.ProjectId,
		OrganizationId: &assistant.OrganizationId,
	}

	vaultCred, err := m.vaultClient.GetCredential(ctx, auth, credentialID)
	if err != nil {
		m.logger.Warnw("Failed to fetch vault credential for registration",
			"assistant_id", assistantID, "credential_id", credentialID, "error", err)
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_status", "config_error")
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_error", "vault credential not found")
		return
	}

	sipConfig, err := sip_infra.ParseConfigFromVault(vaultCred)
	if err != nil {
		m.logger.Warnw("Failed to parse SIP config for registration",
			"assistant_id", assistantID, "error", err)
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_status", "config_error")
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_error", "invalid SIP config: "+err.Error())
		return
	}

	if m.cfg.SIPConfig != nil {
		sipConfig.ApplyOperationalDefaults(
			m.cfg.SIPConfig.Port,
			sip_infra.Transport(m.cfg.SIPConfig.Transport),
			m.cfg.SIPConfig.RTPPortRangeStart,
			m.cfg.SIPConfig.RTPPortRangeEnd,
		)
	}

	if err := m.registrationClient.Register(ctx, &sip_infra.Registration{
		DID:         did,
		Config:      sipConfig,
		AssistantID: assistantID,
	}); err != nil {
		// Permanent SIP rejection (403, 404, 405, etc.) — will never succeed
		if errors.Is(err, sip_infra.ErrPermanentFailure) {
			m.logger.Errorw("SIP registration permanently rejected — will not retry",
				"did", did, "assistant_id", assistantID, "error", err)
			m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_status", "rejected")
			m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_error", err.Error())
			return
		}
		// Auth failure (401/407 after digest) — wrong credentials
		if errors.Is(err, sip_infra.ErrAuthFailed) {
			m.logger.Errorw("SIP registration auth failed — marking deployment as failed",
				"did", did, "assistant_id", assistantID, "error", err)
			m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_status", "failed")
			m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_error", err.Error())
			return
		}
		// Transient failure (timeout, 5xx) — retry with counter
		m.handleTransientFailure(ctx, did, assistantID, deploymentID, err)
		return
	}

	// Success → mark as active, clear error and retry counter
	m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_status", "active")
	m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_error", "")
	m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_retry_count", "0")

	m.logger.Infow("SIP DID registered",
		"did", did,
		"assistant_id", assistantID,
		"server", sipConfig.Server)
}

// upsertDeploymentOption writes a key-value option to a phone deployment using the
// same GORM upsert pattern as CreatePhoneDeployment.
func (m *SIPEngine) upsertDeploymentOption(ctx context.Context, deploymentID uint64, key, value string) {
	db := m.postgres.DB(ctx)
	opt := &internal_assistant_entity.AssistantDeploymentTelephonyOption{
		AssistantDeploymentTelephonyId: deploymentID,
		Metadata: gorm_models.Metadata{
			Key:   key,
			Value: value,
		},
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "assistant_deployment_telephony_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_date"}),
	}).Create(opt).Error; err != nil {
		m.logger.Warnw("Failed to upsert deployment option", "deployment_id", deploymentID, "key", key, "error", err)
	}
}

const maxTransientRetries = 10 // ~50 minutes of retrying (10 × 5-min poll)

// handleTransientFailure increments the retry counter for a transient SIP failure
// (e.g., transport timeout, 5xx). After maxTransientRetries, marks the deployment
// as "unreachable" so reconciliation stops retrying.
func (m *SIPEngine) handleTransientFailure(ctx context.Context, did string, assistantID, deploymentID uint64, err error) {
	db := m.postgres.DB(ctx)
	var opt internal_assistant_entity.AssistantDeploymentTelephonyOption
	retryCount := 0
	if dbErr := db.Where("assistant_deployment_telephony_id = ? AND key = ?",
		deploymentID, "rapida.sip_retry_count").First(&opt).Error; dbErr == nil {
		retryCount, _ = strconv.Atoi(opt.Value)
	}
	retryCount++

	if retryCount >= maxTransientRetries {
		m.logger.Errorw("SIP registration unreachable after max retries — will not retry",
			"did", did, "assistant_id", assistantID, "retries", retryCount, "error", err)
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_status", "unreachable")
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_error", err.Error())
		m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_retry_count", strconv.Itoa(retryCount))
		return
	}

	m.logger.Warnw("SIP registration failed (will retry)",
		"did", did, "assistant_id", assistantID, "retry", retryCount, "error", err)
	m.upsertDeploymentOption(ctx, deploymentID, "rapida.sip_retry_count", strconv.Itoa(retryCount))
}

// RegisterAssistant registers a specific assistant's DID with its SIP provider.
// Called dynamically when a phone deployment is created or updated.
func (m *SIPEngine) RegisterAssistant(ctx context.Context, auth types.SimplePrinciple, assistantID uint64) error {
	if m.registrationClient == nil {
		return fmt.Errorf("registration client not initialized")
	}

	assistant, err := m.assistantService.Get(ctx, auth, assistantID, nil,
		&internal_services.GetAssistantOption{InjectPhoneDeployment: true})
	if err != nil {
		return fmt.Errorf("failed to load assistant %d: %w", assistantID, err)
	}
	if assistant.AssistantPhoneDeployment == nil {
		return fmt.Errorf("no phone deployment for assistant %d", assistantID)
	}
	if assistant.AssistantPhoneDeployment.TelephonyProvider != "sip" {
		return nil // Not a SIP deployment
	}

	opts := assistant.AssistantPhoneDeployment.GetOptions()
	did, _ := opts.GetString("phone")
	if did == "" {
		return fmt.Errorf("phone deployment has no DID configured")
	}

	sipInbound, _ := opts.GetString("rapida.sip_inbound")
	if sipInbound != "true" {
		return nil // Outbound-only — skip registration
	}

	sipConfig, _, err := m.fetchSIPConfigAndVaultCredential(auth, assistant)
	if err != nil {
		return fmt.Errorf("failed to fetch SIP config: %w", err)
	}

	return m.registrationClient.Register(ctx, &sip_infra.Registration{
		DID:         sip_infra.NormalizeDID(did),
		Config:      sipConfig,
		AssistantID: assistantID,
	})
}

func (m *SIPEngine) UnregisterAssistant(ctx context.Context, did string) error {
	if m.registrationClient == nil {
		return nil
	}
	return m.registrationClient.Unregister(ctx, did)
}

// pipelineCreateConversation creates a conversation for inbound calls.
// For outbound calls, the conversation is already created by the channel pipeline.
func (m *SIPEngine) pipelineCreateConversation(ctx context.Context, auth types.SimplePrinciple, assistantID uint64, fromURI string, direction string) (uint64, error) {
	dirEnum := type_enums.DIRECTION_INBOUND

	if direction == "outbound" {
		dirEnum = type_enums.DIRECTION_OUTBOUND
	}

	// Normalize SIP URI to phone number for caller identity
	callerNumber := sip_infra.ExtractDIDFromURI(fromURI)
	if callerNumber == "" {
		callerNumber = fromURI
	}

	assistant, err := m.assistantService.Get(ctx, auth, assistantID, utils.GetVersionDefinition("latest"),
		&internal_services.GetAssistantOption{InjectPhoneDeployment: true})
	if err != nil {
		return 0, fmt.Errorf("failed to load assistant %d: %w", assistantID, err)
	}

	conversation, err := m.assistantConversationService.CreateConversation(
		ctx, auth, callerNumber, assistant.Id, assistant.AssistantProviderId, dirEnum, utils.PhoneCall,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create conversation: %w", err)
	}
	return conversation.Id, nil
}

// pipelineCallSetup builds the CallSetupResult from auth and IDs.
// Pure function — conversation must already exist (created by pipeline or channel layer).
func (m *SIPEngine) pipelineCallSetup(ctx context.Context, session *sip_infra.Session, auth types.SimplePrinciple, assistantID uint64, conversationID uint64) (*sip_pipeline.CallSetupResult, error) {
	assistant := session.GetAssistant()
	if assistant == nil {
		var err error
		assistant, err = m.assistantService.Get(ctx, auth, assistantID, utils.GetVersionDefinition("latest"),
			&internal_services.GetAssistantOption{InjectPhoneDeployment: true})
		if err != nil {
			return nil, fmt.Errorf("failed to load assistant %d: %w", assistantID, err)
		}
	}

	result := &sip_pipeline.CallSetupResult{
		AssistantID:         assistant.Id,
		ConversationID:      conversationID,
		AssistantProviderId: assistant.AssistantProviderId,
		AuthToken:           auth.GetCurrentToken(),
		AuthType:            auth.Type(),
	}
	if auth.GetCurrentProjectId() != nil {
		result.ProjectID = *auth.GetCurrentProjectId()
	}
	if auth.GetCurrentOrganizationId() != nil {
		result.OrganizationID = *auth.GetCurrentOrganizationId()
	}

	return result, nil
}

// pipelineCallStart creates a streamer and talker, then blocks on talker.Talk
// until the call ends. Returns error for setup failures (streamer/talker creation);
// nil means the call connected and Talk() completed normally or via disconnect.
func (m *SIPEngine) pipelineCallStart(ctx context.Context, session *sip_infra.Session, setup *sip_pipeline.CallSetupResult, vaultCred interface{}, sipConfig *sip_infra.Config, direction string) error {
	callID := session.GetCallID()
	isOutbound := session.GetInfo().Direction == sip_infra.CallDirectionOutbound

	// Outbound: ensure session.End() so handleOutboundDialog can proceed.
	if isOutbound {
		defer func() {
			if !session.IsEnded() {
				session.End()
			}
		}()
	}

	if session.IsEnded() {
		m.logger.Warnw("Session already ended before call start", "call_id", callID)
		return fmt.Errorf("session_ended_before_start")
	}

	auth := session.GetAuth()

	cc := &callcontext.CallContext{
		AssistantID:         setup.AssistantID,
		ConversationID:      setup.ConversationID,
		AssistantProviderId: setup.AssistantProviderId,
		AuthToken:           setup.AuthToken,
		AuthType:            setup.AuthType,
		Direction:           direction,
		Provider:            "sip",
		ChannelUUID:         callID,
		ProjectID:           setup.ProjectID,
		OrganizationID:      setup.OrganizationID,
	}

	callCtx, cancel := context.WithCancel(session.Context())
	defer cancel()

	// For outbound calls, session.End() is deferred until this function returns,
	// but BYE cancels the dialog context — not the session context. Bridge the gap
	// by watching ByeReceived() and cancelling callCtx so talker.Talk() can exit.
	go func() {
		select {
		case <-session.ByeReceived():
			cancel()
		case <-callCtx.Done():
		}
	}()

	select {
	case <-session.ByeReceived():
		m.logger.Infow("BYE received before call start", "call_id", callID)
		return fmt.Errorf("bye_before_start")
	default:
	}

	var vc *protos.VaultCredential
	if v, ok := vaultCred.(*protos.VaultCredential); ok {
		vc = v
	} else {
		vc = session.GetVaultCredential()
	}

	streamer, err := internal_telephony.Telephony(internal_telephony.SIP).
		NewStreamer(m.logger, cc, vc, internal_telephony.StreamerOption{
			Ctx:        callCtx,
			SIPSession: session,
			SIPConfig:  sipConfig,
		})
	if err != nil {
		m.logger.Error("Failed to create SIP streamer", "error", err, "call_id", callID)
		return fmt.Errorf("streamer_failed: %w", err)
	}

	if session.IsEnded() {
		if closeable, ok := streamer.(io.Closer); ok {
			closeable.Close()
		}
		return fmt.Errorf("session_ended_after_streamer")
	}

	type transferable interface {
		SetOnTransferInitiated(func(string))
		ExitTransferMode()
	}
	if ts, ok := streamer.(transferable); ok {
		ts.SetOnTransferInitiated(func(target string) {
			m.dispatcher.OnPipeline(m.ctx, sip_infra.TransferInitiatedPipeline{
				ID:        callID,
				Session:   session,
				TargetURI: target,
				Config:    sipConfig,
				OnConnected: func() { cancel() },
				OnFailed:    func() { ts.ExitTransferMode() },
			})
		})
	}

	talker, err := internal_adapter.GetTalker(
		utils.PhoneCall, callCtx, m.cfg, m.logger,
		m.postgres, m.opensearch, m.redis, m.storage, streamer,
	)
	if err != nil {
		if closeable, ok := streamer.(io.Closer); ok {
			closeable.Close()
		}
		m.logger.Error("Failed to create SIP talker", "error", err, "call_id", callID)
		return fmt.Errorf("talker_failed: %w", err)
	}

	m.logger.Infow("SIP call started",
		"call_id", callID,
		"assistant_id", cc.AssistantID,
		"conversation_id", cc.ConversationID,
		"direction", direction)

	if err := talker.Talk(callCtx, auth); err != nil {
		m.logger.Warnw("SIP talker exited", "error", err, "call_id", callID)
	}

	m.logger.Infow("SIP call ended", "call_id", callID)
	return nil
}

func (m *SIPEngine) pipelineCallEnd(callID string) {
	m.mu.RLock()
	srv := m.server
	m.mu.RUnlock()
	if srv == nil {
		return
	}
	session, ok := srv.GetSession(callID)
	if !ok {
		return
	}
	// Don't end the session if a bridge transfer is active — the pipeline
	// transfer handler owns the session lifecycle during bridging.
	if session.GetState() == sip_infra.CallStateTransferring || session.GetState() == sip_infra.CallStateBridgeConnected {
		m.logger.Infow("pipelineCallEnd: skipping session.End — transfer active", "call_id", callID)
		return
	}
	session.End()
}

func (m *SIPEngine) createObserver(ctx context.Context, setup *sip_pipeline.CallSetupResult, auth types.SimplePrinciple) *observe.ConversationObserver {
	meta := observe.SessionMeta{
		AssistantID:             setup.AssistantID,
		AssistantConversationID: setup.ConversationID,
		ProjectID:               setup.ProjectID,
		OrganizationID:          setup.OrganizationID,
	}
	var eventExporters []observe.EventExporter
	var metricExporters []observe.MetricExporter
	if m.cfg.TelemetryConfig != nil {
		if envType := m.cfg.TelemetryConfig.Type(); envType != "" {
			evtExp, metExp, err := observe_exporters.GetExporter(
				ctx, m.logger, &m.cfg.AppConfig, m.opensearch, string(envType), m.cfg.TelemetryConfig.ToMap(),
			)
			if err != nil {
				m.logger.Warnf("SIP observer: default exporter creation failed: %v", err)
			} else if evtExp != nil && metExp != nil {
				eventExporters = append(eventExporters, evtExp)
				metricExporters = append(metricExporters, metExp)
			}
		}
	}
	return observe.NewConversationObserver(&observe.ConversationObserverConfig{
		Logger:         m.logger,
		Auth:           auth,
		AssistantID:    setup.AssistantID,
		ConversationID: setup.ConversationID,
		ProjectID:      setup.ProjectID,
		OrganizationID: setup.OrganizationID,
		Persist:        m.assistantConversationService,
		Events:         observe.NewEventCollector(m.logger, meta, eventExporters...),
		Metrics:        observe.NewMetricCollector(m.logger, meta, metricExporters...),
	})
}
