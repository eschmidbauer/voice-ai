// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"context"

	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
)

func (d *Dispatcher) handleInviteReceived(ctx context.Context, v sip_infra.InviteReceivedPipeline) {
	sdpInfo, err := d.server.ParseSDP(v.SDPBody)
	if err != nil {
		d.logger.Warnw("Failed to parse SDP, using defaults", "error", err, "call_id", v.ID)
		pcmu := sip_infra.CodecPCMU
		sdpInfo = &sip_infra.SDPMediaInfo{PreferredCodec: &pcmu}
	}

	did := sip_infra.ExtractDIDFromURI(v.ToURI)
	if did == "" {
		did = sip_infra.ExtractDIDFromURI(v.FromURI)
	}

	d.OnPipeline(ctx, sip_infra.RouteResolvedPipeline{
		ID:      v.ID,
		DID:     did,
		SDP:     sdpInfo,
		FromURI: v.FromURI,
		ToURI:   v.ToURI,
		Req:     v.Req,
		Tx:      v.Tx,
	})
}

func (d *Dispatcher) handleRouteResolved(ctx context.Context, v sip_infra.RouteResolvedPipeline) {
	if v.DID == "" {
		d.logger.Warnw("No DID found in INVITE", "call_id", v.ID)
		d.sendReject(v.Tx, v.Req, 404)
		return
	}

	assistantID, auth, err := d.resolveAssistantByDID(v.DID)
	if err != nil {
		d.logger.Warnw("DID lookup failed", "call_id", v.ID, "did", v.DID, "error", err)
		d.sendReject(v.Tx, v.Req, 404)
		return
	}

	d.OnPipeline(ctx, sip_infra.AuthenticatedPipeline{
		ID:          v.ID,
		AssistantID: assistantID,
		Auth:        auth,
		SDP:         v.SDP,
		FromURI:     v.FromURI,
		Req:         v.Req,
		Tx:          v.Tx,
	})
}

// handleAuthenticated is a future extension point for pipeline-native session creation.
// Currently server.go handles SIP protocol via the middleware chain.
func (d *Dispatcher) handleAuthenticated(ctx context.Context, v sip_infra.AuthenticatedPipeline) {
	d.logger.Infow("Pipeline: Authenticated",
		"call_id", v.ID,
		"assistant_id", v.AssistantID)
}
