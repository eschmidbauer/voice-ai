// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"context"
	"strings"

	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
)

func (d *Dispatcher) handleTransferInitiated(ctx context.Context, v sip_infra.TransferInitiatedPipeline) {
	go d.executeTransfer(ctx, v)
}

func (d *Dispatcher) executeTransfer(ctx context.Context, v sip_infra.TransferInitiatedPipeline) {
	d.logger.Infow("Pipeline: TransferInitiated",
		"call_id", v.ID, "target", v.TargetURI)

	if d.server == nil {
		d.logger.Errorw("Pipeline: transfer failed — SIP server not available", "call_id", v.ID)
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
		d.logger.Errorw("Pipeline: transfer outbound call failed",
			"call_id", v.ID, "target", v.TargetURI, "error", err)
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

	d.logger.Infow("Pipeline: transfer target answered",
		"call_id", v.ID,
		"outbound_call_id", outboundSession.GetCallID(),
		"target", v.TargetURI)

	if v.OnConnected != nil {
		v.OnConnected(outboundSession.GetRTPHandler())
	}

	v.Session.SetState(sip_infra.CallStateBridgeConnected)

	d.OnPipeline(ctx, sip_infra.TransferConnectedPipeline{
		ID:              v.ID,
		InboundSession:  v.Session,
		OutboundSession: outboundSession,
	})

	if err := d.server.BridgeTransfer(context.Background(), v.Session, outboundSession); err != nil {
		d.logger.Errorw("Pipeline: bridge failed",
			"call_id", v.ID, "error", err)
		v.Session.SetMetadata(sip_infra.MetadataBridgeTransferStatus, "failed")
		return
	}
	v.Session.SetMetadata(sip_infra.MetadataBridgeTransferStatus, "completed")
}

func (d *Dispatcher) handleTransferConnected(ctx context.Context, v sip_infra.TransferConnectedPipeline) {
	d.logger.Infow("Pipeline: TransferConnected",
		"call_id", v.ID,
		"outbound_call_id", v.OutboundSession.GetCallID())
}

func (d *Dispatcher) handleTransferFailed(ctx context.Context, v sip_infra.TransferFailedPipeline) {
	d.logger.Warnw("Pipeline: TransferFailed",
		"call_id", v.ID, "reason", v.Reason, "error", v.Error)
}
