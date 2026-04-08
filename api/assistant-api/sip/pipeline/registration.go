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

func (d *Dispatcher) handleRegisterRequested(ctx context.Context, v sip_infra.RegisterRequestedPipeline) {
	if d.registrationClient == nil {
		d.logger.Warnw("Pipeline: RegisterRequested but no registration client", "did", v.DID)
		return
	}

	if err := d.registrationClient.Register(ctx, v.Registration); err != nil {
		// Log directly — do NOT call d.OnPipeline here. This handler runs on the
		// control dispatcher goroutine, and RegisterFailed routes back to controlCh.
		// If controlCh is full, the send blocks the same goroutine that drains it → deadlock.
		d.logger.Warnw("Pipeline: RegisterFailed",
			"did", v.DID,
			"error", err)
		return
	}

	d.logger.Infow("Pipeline: RegisterActive",
		"did", v.DID,
		"assistant_id", v.Registration.AssistantID)
}

func (d *Dispatcher) handleRegisterActive(ctx context.Context, v sip_infra.RegisterActivePipeline) {
	d.logger.Infow("Pipeline: RegisterActive",
		"did", v.DID,
		"assistant_id", v.AssistantID)
}

func (d *Dispatcher) handleRegisterFailed(ctx context.Context, v sip_infra.RegisterFailedPipeline) {
	d.logger.Warnw("Pipeline: RegisterFailed",
		"did", v.DID,
		"error", v.Error)
}

func (d *Dispatcher) handleRegisterExpiring(ctx context.Context, v sip_infra.RegisterExpiringPipeline) {
	d.logger.Debugw("Pipeline: RegisterExpiring", "did", v.DID)
}
