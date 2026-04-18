// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_sip_telephony

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rapidaai/api/assistant-api/config"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/types"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

const sipProvider = "sip"
const defaultOutboundSIPPort = 5060

type sipTelephony struct {
	appCfg       *config.AssistantConfig
	logger       commons.Logger
	sharedServer *sip_infra.Server
}

func NewSIPTelephony(cfg *config.AssistantConfig, logger commons.Logger, sipServer *sip_infra.Server) (internal_type.Telephony, error) {
	return &sipTelephony{
		appCfg:       cfg,
		logger:       logger,
		sharedServer: sipServer,
	}, nil
}

func (t *sipTelephony) parseConfig(vaultCredential *protos.VaultCredential) (*sip_infra.Config, error) {
	cfg, err := sip_infra.ParseConfigFromVault(vaultCredential)
	if err != nil {
		return nil, err
	}

	if cfg.Port <= 0 {
		cfg.Port = defaultOutboundSIPPort
	}

	if t.appCfg.SIPConfig != nil {
		cfg.ApplyOperationalDefaults(
			t.appCfg.SIPConfig.Port,
			sip_infra.Transport(t.appCfg.SIPConfig.Transport),
			t.appCfg.SIPConfig.RTPPortRangeStart,
			t.appCfg.SIPConfig.RTPPortRangeEnd,
		)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (t *sipTelephony) StatusCallback(
	c *gin.Context,
	auth types.SimplePrinciple,
	assistantId uint64,
	assistantConversationId uint64,
) (*internal_type.StatusInfo, error) {
	payload := make(map[string]interface{})

	if body, err := c.GetRawData(); err == nil && len(body) > 0 {
		if json.Unmarshal(body, &payload) != nil {
			// Not JSON — try form-encoded
			if formErr := c.Request.ParseForm(); formErr == nil {
				for k, v := range c.Request.PostForm {
					payload[k] = v[0]
				}
			}
		}
	}
	if len(payload) == 0 {
		for k, v := range c.Request.URL.Query() {
			payload[k] = v[0]
		}
	}

	eventType, _ := payload["event"].(string)
	callID, _ := payload["call_id"].(string)

	t.logger.Debug("SIP status callback received",
		"event", eventType,
		"call_id", callID,
		"assistant_id", assistantId,
		"conversation_id", assistantConversationId)

	return &internal_type.StatusInfo{Event: eventType, Payload: payload}, nil
}

func (t *sipTelephony) CatchAllStatusCallback(ctx *gin.Context) (*internal_type.StatusInfo, error) {
	return nil, nil
}

func (t *sipTelephony) OutboundCall(
	auth types.SimplePrinciple,
	toPhone string,
	fromPhone string,
	assistant *internal_assistant_entity.Assistant,
	assistantConversationId uint64,
	vaultCredential *protos.VaultCredential,
	opts utils.Option,
) (*internal_type.CallInfo, error) {
	info := &internal_type.CallInfo{Provider: sipProvider}

	cfg, err := t.parseConfig(vaultCredential)
	if err != nil {
		info.Status = "FAILED"
		info.ErrorMessage = fmt.Sprintf("config error: %s", err.Error())
		return info, err
	}

	if t.sharedServer == nil {
		info.Status = "FAILED"
		info.ErrorMessage = "SIP server not initialized"
		return info, fmt.Errorf("shared SIP server not available")
	}
	if !t.sharedServer.IsRunning() {
		info.Status = "FAILED"
		info.ErrorMessage = "SIP server not running"
		return info, fmt.Errorf("shared SIP server is not running")
	}

	contextID, _ := opts.GetString("rapida.context_id")
	session, err := t.sharedServer.MakeCall(context.Background(), cfg, toPhone, fromPhone, sip_infra.MakeCallOptions{
		Auth:            auth,
		Assistant:       assistant,
		ConversationID:  assistantConversationId,
		ContextID:       contextID,
		VaultCredential: vaultCredential,
	})
	if err != nil {
		info.Status = "FAILED"
		info.ErrorMessage = fmt.Sprintf("call error: %s", err.Error())
		return info, err
	}

	t.logger.Info("SIP outbound call initiated",
		"to", toPhone,
		"from", fromPhone,
		"call_id", session.GetCallID(),
		"assistant_id", assistant.Id,
		"conversation_id", assistantConversationId)

	info.ChannelUUID = session.GetCallID()
	info.Status = "SUCCESS"
	info.StatusInfo = internal_type.StatusInfo{
		Event: "initiated",
		Payload: map[string]interface{}{
			"to":              toPhone,
			"from":            fromPhone,
			"call_id":         session.GetCallID(),
			"assistant_id":    assistant.Id,
			"conversation_id": assistantConversationId,
		},
	}
	info.Extra = map[string]string{
		"telephony.status": "initiated",
	}
	return info, nil
}

func (t *sipTelephony) InboundCall(
	c *gin.Context,
	auth types.SimplePrinciple,
	assistantId uint64,
	clientNumber string,
	assistantConversationId uint64,
) error {
	c.JSON(http.StatusOK, gin.H{
		"status":          "ready",
		"assistant_id":    assistantId,
		"conversation_id": assistantConversationId,
		"client_number":   clientNumber,
		"message":         "SIP inbound call ready - connect via SIP signaling",
	})
	return nil
}

func (t *sipTelephony) ReceiveCall(c *gin.Context) (*internal_type.CallInfo, error) {
	clientNumber := c.Query("from")
	if clientNumber == "" {
		clientNumber = c.Query("caller")
	}
	if clientNumber == "" {
		return nil, fmt.Errorf("missing caller information")
	}

	queryParams := make(map[string]string, len(c.Request.URL.Query()))
	for key, values := range c.Request.URL.Query() {
		queryParams[key] = values[0]
	}

	info := &internal_type.CallInfo{
		CallerNumber: clientNumber,
		Provider:     sipProvider,
		Status:       "SUCCESS",
		StatusInfo:   internal_type.StatusInfo{Event: "webhook", Payload: queryParams},
	}
	if callID := c.Query("call_id"); callID != "" {
		info.ChannelUUID = callID
	}
	return info, nil
}
