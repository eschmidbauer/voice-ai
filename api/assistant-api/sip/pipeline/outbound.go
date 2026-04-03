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

func (d *Dispatcher) handleOutboundRequested(ctx context.Context, v sip_infra.OutboundRequestedPipeline) {
	d.logger.Infow("Pipeline: OutboundRequested",
		"call_id", v.ID,
		"to", v.ToPhone,
		"from", v.FromPhone)
}

func (d *Dispatcher) handleInviteSent(ctx context.Context, v sip_infra.InviteSentPipeline) {
	d.logger.Infow("Pipeline: InviteSent",
		"call_id", v.ID,
		"to", v.ToPhone)
}

func (d *Dispatcher) handleAnswerReceived(ctx context.Context, v sip_infra.AnswerReceivedPipeline) {
	d.logger.Infow("Pipeline: AnswerReceived", "call_id", v.ID)
}
