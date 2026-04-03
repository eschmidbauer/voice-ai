// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"context"
	"time"

	obs "github.com/rapidaai/api/assistant-api/internal/observe"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
	"github.com/rapidaai/pkg/types"
)

// handleSessionEstablished converges inbound and outbound calls.
// Creates conversation, observer, hooks, then launches Talk() in a goroutine.
func (d *Dispatcher) handleSessionEstablished(ctx context.Context, v sip_infra.SessionEstablishedPipeline) {
	d.logger.Infow("Pipeline: SessionEstablished",
		"call_id", v.ID,
		"direction", v.Direction,
		"assistant_id", v.AssistantID)

	if d.onCallSetup == nil || d.onCallStart == nil {
		d.logger.Error("Pipeline: callbacks not configured", "call_id", v.ID)
		d.OnPipeline(ctx, sip_infra.CallFailedPipeline{
			ID:    v.ID,
			Error: sip_infra.ErrConnectionFailed,
		})
		return
	}

	setup, err := d.onCallSetup(ctx, v.Session, v.Auth, v.AssistantID, v.FromURI, string(v.Direction))
	if err != nil {
		d.logger.Error("Pipeline: call setup failed", "call_id", v.ID, "error", err)
		d.OnPipeline(ctx, sip_infra.CallFailedPipeline{ID: v.ID, Error: err})
		return
	}

	if d.onCreateObserver != nil {
		o := d.onCreateObserver(ctx, setup, v.Auth)
		if o != nil {
			d.storeObserver(v.ID, o)
		}
	}

	if d.onCreateHooks != nil {
		hooks := d.onCreateHooks(ctx, v.Auth, v.AssistantID, setup.ConversationID)
		if hooks != nil {
			d.storeHooks(v.ID, hooks)
			hooks.OnBegin(ctx)
		}
	}

	if o, ok := d.getObserver(v.ID); ok && v.FromURI != "" {
		o.EmitMetadata(ctx, []*types.Metadata{
			types.NewMetadata("sip.caller_uri", v.FromURI),
		})
	}

	d.OnPipeline(ctx,
		sip_infra.CallStartedPipeline{ID: v.ID, Session: v.Session},
		sip_infra.EventEmittedPipeline{ID: v.ID, Event: obs.EventCallStarted, Data: map[string]string{
			obs.DataDirection: string(v.Direction),
		}},
	)

	go func() {
		startTime := time.Now()
		defer func() {
			if r := recover(); r != nil {
				d.logger.Error("Pipeline: onCallStart panicked", "call_id", v.ID, "panic", r)
			}
			d.OnPipeline(ctx, sip_infra.CallEndedPipeline{
				ID:       v.ID,
				Duration: time.Since(startTime),
				Reason:   "talk_completed",
			})
		}()
		d.onCallStart(ctx, v.Session, setup, v.VaultCredential, v.Config, string(v.Direction))
	}()
}

func (d *Dispatcher) handleCallStarted(ctx context.Context, v sip_infra.CallStartedPipeline) {
	d.logger.Infow("Pipeline: CallStarted", "call_id", v.ID)
}

func (d *Dispatcher) handleHoldRequested(ctx context.Context, v sip_infra.HoldRequestedPipeline) {
	action := "hold"
	if !v.IsHold {
		action = "resume"
	}
	d.logger.Infow("Pipeline: HoldRequested", "call_id", v.ID, "action", action)
	d.OnPipeline(ctx, sip_infra.EventEmittedPipeline{ID: v.ID, Event: action})
}

func (d *Dispatcher) handleReInviteReceived(ctx context.Context, v sip_infra.ReInviteReceivedPipeline) {
	d.logger.Debugw("Pipeline: ReInviteReceived", "call_id", v.ID)
}
