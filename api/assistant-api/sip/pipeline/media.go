// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"context"
	"fmt"
	"time"

	obs "github.com/rapidaai/api/assistant-api/internal/observe"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
)

func (d *Dispatcher) handleSessionEstablished(ctx context.Context, v sip_infra.SessionEstablishedPipeline) {
	d.logger.Infow("Pipeline: SessionEstablished",
		"call_id", v.ID,
		"direction", v.Direction,
		"assistant_id", v.AssistantID,
		"conversation_id", v.ConversationID)

	if d.onCallSetup == nil || d.onCallStart == nil {
		d.logger.Error("Pipeline: callbacks not configured", "call_id", v.ID)
		v.Session.End()
		return
	}

	// Resolve conversation ID:
	// - Outbound: already created by channel pipeline, passed in ConversationID
	// - Inbound: create now via onCreateConversation
	conversationID := v.ConversationID
	if conversationID == 0 {
		if d.onCreateConversation == nil {
			d.logger.Error("Pipeline: onCreateConversation not configured", "call_id", v.ID)
			v.Session.End()
			return
		}
		var err error
		conversationID, err = d.onCreateConversation(ctx, v.Auth, v.AssistantID, v.FromURI, string(v.Direction))
		if err != nil {
			d.logger.Error("Pipeline: create conversation failed", "call_id", v.ID, "error", err)
			v.Session.End()
			return
		}
		v.Session.SetConversationID(conversationID)
	}

	setup, err := d.onCallSetup(ctx, v.Session, v.Auth, v.AssistantID, conversationID)
	if err != nil {
		d.logger.Error("Pipeline: call setup failed", "call_id", v.ID, "error", err)
		v.Session.End()
		return
	}

	var observer *obs.ConversationObserver
	if d.onCreateObserver != nil {
		observer = d.onCreateObserver(ctx, setup, v.Auth)
	}

	var hooks *obs.ConversationHooks
	if d.onCreateHooks != nil {
		hooks = d.onCreateHooks(ctx, v.Auth, v.AssistantID, setup.ConversationID)
		if hooks != nil {
			hooks.OnBegin(ctx)
		}
	}

	if observer != nil {
		clientPhone := sip_infra.ExtractDIDFromURI(v.FromURI)
		if clientPhone == "" {
			clientPhone = v.FromURI
		}
		// Assistant phone = our DID (To URI for inbound, From URI for outbound)
		assistantPhone := ""
		if info := v.Session.GetInfo(); info.LocalURI != "" {
			assistantPhone = sip_infra.ExtractDIDFromURI(info.LocalURI)
		}
		codec := ""
		sampleRate := ""
		if negotiated := v.Session.GetNegotiatedCodec(); negotiated != nil {
			codec = negotiated.Name
			sampleRate = fmt.Sprintf("%d", negotiated.ClockRate)
		}
		observer.EmitMetadata(ctx, obs.ClientMetadata(
			clientPhone, assistantPhone, string(v.Direction), "sip",
			v.ID, "", codec, sampleRate,
		))
		observer.EmitEvent(ctx, obs.ComponentTelephony, map[string]string{
			obs.DataType:      obs.EventCallStarted,
			obs.DataProvider:  "sip",
			obs.DataDirection: string(v.Direction),
		})
		d.logger.Infow("Pipeline: call_started event emitted",
			"call_id", v.ID, "direction", v.Direction)
	}

	go func() {
		startTime := time.Now()
		reason := "talk_completed"
		status := "COMPLETED"
		defer func() {
			if r := recover(); r != nil {
				reason = fmt.Sprintf("panic: %v", r)
				status = "FAILED"
				d.logger.Error("Pipeline: onCallStart panicked", "call_id", v.ID, "panic", r)
			}

			if observer != nil {
				observer.EmitEvent(ctx, obs.ComponentTelephony, map[string]string{
					obs.DataType:      obs.EventCallEnded,
					obs.DataProvider:  "sip",
					obs.DataDirection: string(v.Direction),
					obs.DataReason:    reason,
				})
				observer.EmitMetric(ctx, obs.CallStatusMetric(status, reason))
				observer.Shutdown(ctx)
			}
			if hooks != nil {
				hooks.OnEnd(ctx)
			}
			if d.onCallEnd != nil {
				d.onCallEnd(v.ID)
			}

			d.OnPipeline(ctx, sip_infra.CallEndedPipeline{
				ID:       v.ID,
				Duration: time.Since(startTime),
				Reason:   reason,
			})
		}()
		if err := d.onCallStart(ctx, v.Session, setup, v.VaultCredential, v.Config, string(v.Direction)); err != nil {
			reason = err.Error()
			status = "FAILED"
		}

		// Check if the call ended due to a bridge transfer — emit transfer events
		if observer != nil {
			if targetVal, ok := v.Session.GetMetadata(sip_infra.MetadataBridgeTransferTarget); ok {
				if target, ok := targetVal.(string); ok && target != "" {
					transferStatus := "failed"
					if statusVal, ok := v.Session.GetMetadata(sip_infra.MetadataBridgeTransferStatus); ok {
						if s, ok := statusVal.(string); ok {
							transferStatus = s
						}
					}
					reason = "transfer_" + transferStatus
					d.logger.Infow("Pipeline: bridge transfer",
						"call_id", v.ID, "target", target, "status", transferStatus)
					observer.EmitEvent(ctx, obs.ComponentTelephony, map[string]string{
						obs.DataType:      obs.EventTransferRequested,
						obs.DataProvider:  "sip",
						obs.DataDirection: string(v.Direction),
						obs.DataTo:        target,
						obs.DataReason:    transferStatus,
					})
				}
			}
		}
	}()
}
