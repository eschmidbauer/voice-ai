// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"fmt"

	"github.com/emiago/sipgo/sip"
	"github.com/rapidaai/pkg/types"
)

func (d *Dispatcher) resolveAssistantByDID(did string) (uint64, types.SimplePrinciple, error) {
	if d.didResolver == nil {
		return 0, nil, fmt.Errorf("DID resolver not configured")
	}
	return d.didResolver(did)
}

func (d *Dispatcher) sendReject(tx sip.ServerTransaction, req *sip.Request, statusCode int) {
	if tx == nil || req == nil {
		return
	}
	resp := sip.NewResponseFromRequest(req, statusCode, "", nil)
	if err := tx.Respond(resp); err != nil {
		d.logger.Warnw("Failed to send SIP reject", "error", err, "status", statusCode)
	}
}
