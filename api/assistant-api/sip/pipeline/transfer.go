// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"context"
	"strings"
	"time"

	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
)

func (d *Dispatcher) handleTransferInitiated(ctx context.Context, v sip_infra.TransferInitiatedPipeline) {
	go d.executeTransfer(ctx, v)
}

func (d *Dispatcher) executeTransfer(ctx context.Context, v sip_infra.TransferInitiatedPipeline) {
	d.logger.Infow("Pipeline: transfer_initiated",
		"call_id", v.ID, "target", v.TargetURI)

	if d.server == nil {
		d.logger.Errorw("Pipeline: transfer_failed — SIP server not available",
			"call_id", v.ID, "target", v.TargetURI, "reason", "server_nil")
		v.Session.SetMetadata(sip_infra.MetadataBridgeTransferStatus, "failed")
		if v.OnFailed != nil {
			v.OnFailed()
		}
		return
	}

	cfg := v.Config
	if cfg == nil {
		cfg = v.Session.GetConfig()
	}

	if cfg.CallerID == "" {
		if assistant := v.Session.GetAssistant(); assistant != nil && assistant.AssistantPhoneDeployment != nil {
			if did, err := assistant.AssistantPhoneDeployment.GetOptions().GetString("phone"); err == nil && did != "" {
				cfg.CallerID = strings.TrimPrefix(did, "+")
			}
		}
	}

	bridgeCtx, bridgeCancel := context.WithTimeout(v.Session.Context(), sip_infra.BridgeCallTimeout)
	defer bridgeCancel()

	outboundSession, err := d.server.MakeBridgeCall(bridgeCtx, cfg, v.TargetURI, cfg.CallerID)
	if err != nil {
		d.logger.Errorw("Pipeline: transfer_failed — outbound call failed",
			"call_id", v.ID,
			"target", v.TargetURI,
			"reason", "outbound_failed",
			"error", err)
		v.Session.SetMetadata(sip_infra.MetadataBridgeTransferStatus, "failed")

		if v.OnFailed != nil {
			v.OnFailed()
		}

		d.OnPipeline(ctx, sip_infra.TransferFailedPipeline{
			ID:     v.ID,
			Error:  err,
			Reason: "outbound_failed",
		})
		return
	}

	outboundCallID := outboundSession.GetCallID()

	d.logger.Infow("Pipeline: transfer_connected",
		"call_id", v.ID,
		"outbound_call_id", outboundCallID,
		"target", v.TargetURI)

	// Store outbound call ID in session metadata for observability
	v.Session.SetMetadata(sip_infra.MetadataBridgeTransferOutboundCallID, outboundCallID)

	if v.OnConnected != nil {
		v.OnConnected(outboundSession.GetRTPHandler())
	}

	v.Session.SetState(sip_infra.CallStateBridgeConnected)

	d.OnPipeline(ctx, sip_infra.TransferConnectedPipeline{
		ID:              v.ID,
		InboundSession:  v.Session,
		OutboundSession: outboundSession,
	})

	// Track bridge duration from the moment the transfer target answered
	bridgeStart := time.Now()

	if err := d.server.BridgeTransfer(context.Background(), v.Session, outboundSession, v.OnOperatorAudio); err != nil {
		bridgeDuration := time.Since(bridgeStart)
		d.logger.Errorw("Pipeline: transfer_completed — bridge failed",
			"call_id", v.ID,
			"target", v.TargetURI,
			"outbound_call_id", outboundCallID,
			"status", "failed",
			"bridge_duration", bridgeDuration,
			"error", err)
		v.Session.SetMetadata(sip_infra.MetadataBridgeTransferStatus, "failed")
		v.Session.SetMetadata(sip_infra.MetadataBridgeTransferDuration, bridgeDuration.String())
	} else {
		bridgeDuration := time.Since(bridgeStart)
		d.logger.Infow("Pipeline: transfer_completed",
			"call_id", v.ID,
			"target", v.TargetURI,
			"outbound_call_id", outboundCallID,
			"status", "completed",
			"bridge_duration", bridgeDuration)
		v.Session.SetMetadata(sip_infra.MetadataBridgeTransferStatus, "completed")
		v.Session.SetMetadata(sip_infra.MetadataBridgeTransferDuration, bridgeDuration.String())
	}

	if v.OnTeardown != nil {
		v.OnTeardown()
	}

	// End the inbound session after metadata is written. This unblocks
	// pipelineCallStart's session wait, which then reads the metadata
	// for observer events. BridgeTransfer only ends the outbound leg.
	if !v.Session.IsEnded() {
		v.Session.End()
	}
}

func (d *Dispatcher) handleTransferConnected(ctx context.Context, v sip_infra.TransferConnectedPipeline) {
	outboundInfo := v.OutboundSession.GetInfo()
	d.logger.Infow("Pipeline: transfer_connected",
		"call_id", v.ID,
		"outbound_call_id", v.OutboundSession.GetCallID(),
		"target_uri", outboundInfo.RemoteURI,
		"codec", outboundInfo.Codec)
}

func (d *Dispatcher) handleTransferFailed(ctx context.Context, v sip_infra.TransferFailedPipeline) {
	// Categorize the failure for structured alerting
	category := categorizeTransferError(v.Reason, v.Error)
	d.logger.Warnw("Pipeline: transfer_failed",
		"call_id", v.ID,
		"reason", v.Reason,
		"category", category,
		"error", v.Error)
}

// categorizeTransferError maps raw transfer failure reasons into high-level
// categories for structured logging and alerting. Categories:
//   - "setup": server unavailable or config errors before dialing
//   - "network": outbound call could not be placed (timeout, DNS, network)
//   - "rejected": callee rejected the call (busy, declined)
//   - "bridge": bridge was established but broke during media relay
//   - "unknown": could not determine category
func categorizeTransferError(reason string, err error) string {
	switch {
	case reason == "server_nil" || reason == "config_error":
		return "setup"
	case reason == "outbound_failed":
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline") {
				return "network"
			}
			if strings.Contains(errMsg, "486") || strings.Contains(errMsg, "603") ||
				strings.Contains(errMsg, "busy") || strings.Contains(errMsg, "declined") {
				return "rejected"
			}
		}
		return "network"
	case reason == "bridge_failed":
		return "bridge"
	default:
		return "unknown"
	}
}
