// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_pipeline

import (
	"context"
	"fmt"

	obs "github.com/rapidaai/api/assistant-api/internal/observe"
	"github.com/rapidaai/protos"
)

func (d *Dispatcher) handleDisconnectRequested(ctx context.Context, v DisconnectRequestedPipeline) {
	d.logger.Infow("Pipeline: DisconnectRequested", "call_id", v.ID, "reason", v.Reason)
	d.emitEvent(ctx, v.ID, obs.ComponentSession, map[string]string{
		obs.DataType:   obs.EventDisconnectRequested,
		obs.DataReason: v.Reason,
	})
}

func (d *Dispatcher) handleCallCompleted(ctx context.Context, v CallCompletedPipeline) {
	d.logger.Infow("Pipeline: CallCompleted",
		"call_id", v.ID,
		"duration", v.Duration,
		"messages", v.Messages,
		"reason", v.Reason)

	d.emitEvent(ctx, v.ID, obs.ComponentSession, map[string]string{
		obs.DataType:     obs.EventCallCompleted,
		obs.DataReason:   v.Reason,
		obs.DataDuration: fmt.Sprintf("%d", v.Duration.Milliseconds()),
		obs.DataMessages: fmt.Sprintf("%d", v.Messages),
	})

	d.emitMetric(ctx, v.ID, []*protos.Metric{
		{Name: obs.MetricCallDurationMs, Value: fmt.Sprintf("%d", v.Duration.Milliseconds()), Description: "Call duration"},
		{Name: obs.MetricCallEndReason, Value: v.Reason, Description: "Call end reason"},
	})

	if hooks, ok := d.getHooks(v.ID); ok {
		hooks.OnEnd(ctx)
		d.removeHooks(v.ID)
	}

	d.removeObserver(ctx, v.ID)
}

func (d *Dispatcher) handleCallFailed(ctx context.Context, v CallFailedPipeline) {
	d.logger.Warnw("Pipeline: CallFailed",
		"call_id", v.ID,
		"stage", v.Stage,
		"error", v.Error)

	d.emitEvent(ctx, v.ID, obs.ComponentSession, map[string]string{
		obs.DataType:  obs.EventCallFailed,
		obs.DataStage: v.Stage,
		obs.DataError: fmt.Sprintf("%v", v.Error),
	})

	d.emitMetric(ctx, v.ID, []*protos.Metric{
		{Name: obs.MetricCallFailed, Value: v.Stage, Description: fmt.Sprintf("Call failed at %s: %v", v.Stage, v.Error)},
	})

	if hooks, ok := d.getHooks(v.ID); ok {
		hooks.OnError(ctx)
		d.removeHooks(v.ID)
	}

	d.removeObserver(ctx, v.ID)
}
