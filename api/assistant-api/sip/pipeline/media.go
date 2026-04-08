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
	"github.com/rapidaai/pkg/types"
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
		observer.EmitMetadata(ctx, []*types.Metadata{
			types.NewMetadata("sip.caller_uri", v.FromURI),
			types.NewMetadata("conversation.direction", string(v.Direction)),
			types.NewMetadata("conversation.provider", "sip"),
		})
		observer.EmitEvent(ctx, obs.ComponentSIP, map[string]string{
			obs.DataType:      obs.EventCallStarted,
			obs.DataDirection: string(v.Direction),
		})
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
				observer.EmitEvent(ctx, obs.ComponentSIP, map[string]string{
					obs.DataType:   obs.EventCallEnded,
					obs.DataReason: reason,
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

			d.logger.Infow("Pipeline: CallEnded",
				"call_id", v.ID,
				"duration", fmt.Sprintf("%dms", time.Since(startTime).Milliseconds()),
				"reason", reason,
				"status", status)
		}()
		if err := d.onCallStart(ctx, v.Session, setup, v.VaultCredential, v.Config, string(v.Direction)); err != nil {
			reason = err.Error()
			status = "FAILED"
		}
	}()
}
