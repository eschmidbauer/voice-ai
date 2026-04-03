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
	"github.com/rapidaai/pkg/types"
	"github.com/rapidaai/protos"
)

func (d *Dispatcher) handleCallReceived(ctx context.Context, v CallReceivedPipeline, resultCh chan<- *PipelineResult) {
	d.logger.Infow("Pipeline: CallReceived", "provider", v.Provider, "assistant_id", v.AssistantID)

	if d.onReceiveCall == nil {
		sendResult(resultCh, &PipelineResult{Error: ErrCallbackNotConfigured})
		return
	}

	callInfo, err := d.onReceiveCall(ctx, v.Provider, v.GinContext)
	if err != nil {
		sendResult(resultCh, &PipelineResult{Error: err})
		return
	}
	if callInfo == nil {
		sendResult(resultCh, &PipelineResult{})
		return
	}

	d.resultChStore(v.ID, resultCh)

	d.OnPipeline(ctx, WebhookParsedPipeline{
		ID:          v.ID,
		Provider:    v.Provider,
		Auth:        v.Auth,
		AssistantID: v.AssistantID,
		CallInfo:    callInfo,
		GinContext:  v.GinContext,
	})
}

func (d *Dispatcher) handleWebhookParsed(ctx context.Context, v WebhookParsedPipeline) {
	d.logger.Infow("Pipeline: WebhookParsed", "caller", v.CallInfo.CallerNumber)

	if d.onLoadAssistant == nil {
		d.failWithResult(v.ID, "webhook_parsed", ErrCallbackNotConfigured)
		return
	}

	assistant, err := d.onLoadAssistant(ctx, v.Auth, v.AssistantID)
	if err != nil {
		d.failWithResult(v.ID, "assistant_load", err)
		return
	}

	d.OnPipeline(ctx, AssistantResolvedPipeline{
		ID:          v.ID,
		Provider:    v.Provider,
		Auth:        v.Auth,
		AssistantID: v.AssistantID,
		Assistant:   assistant,
		CallInfo:    v.CallInfo,
		GinContext:  v.GinContext,
	})
}

func (d *Dispatcher) handleAssistantResolved(ctx context.Context, v AssistantResolvedPipeline) {
	d.logger.Infow("Pipeline: AssistantResolved", "assistant_id", v.AssistantID)

	// Create conversation
	if d.onCreateConversation == nil || d.onSaveCallContext == nil {
		d.failWithResult(v.ID, "assistant_resolved", ErrCallbackNotConfigured)
		return
	}

	conversationID, err := d.onCreateConversation(ctx, v.Auth, v.CallInfo.CallerNumber, v.Assistant.Id, v.Assistant.AssistantProviderId, "inbound")
	if err != nil {
		d.failWithResult(v.ID, "conversation_create", err)
		return
	}

	// Save call context
	contextID, err := d.onSaveCallContext(ctx, v.Auth, v.Assistant, conversationID, v.CallInfo, v.Provider)
	if err != nil {
		d.failWithResult(v.ID, "context_save", err)
		return
	}

	d.OnPipeline(ctx, ConversationCreatedPipeline{
		ID:             contextID,
		Provider:       v.Provider,
		Auth:           v.Auth,
		AssistantID:    v.AssistantID,
		Assistant:      v.Assistant,
		ConversationID: conversationID,
		ContextID:      contextID,
		CallInfo:       v.CallInfo,
		GinContext:     v.GinContext,
	})
}

func (d *Dispatcher) handleConversationCreated(ctx context.Context, v ConversationCreatedPipeline) {
	d.logger.Infow("Pipeline: ConversationCreated",
		"context_id", v.ContextID,
		"conversation_id", v.ConversationID)

	// Create observer for this call
	if d.onCreateObserver != nil {
		o := d.onCreateObserver(ctx, v.ContextID, v.Auth, v.AssistantID, v.ConversationID)
		if o != nil {
			d.storeObserver(v.ContextID, o)
		}
	}

	// Emit creation-time telemetry through observer
	if v.CallInfo != nil {
		// Provider-specific metadata
		if len(v.CallInfo.Extra) > 0 {
			if o, ok := d.getObserver(v.ContextID); ok {
				metadata := make([]*types.Metadata, 0, len(v.CallInfo.Extra))
				for k, val := range v.CallInfo.Extra {
					metadata = append(metadata, types.NewMetadata(k, val))
				}
				o.EmitMetadata(ctx, metadata)
			}
		}

		// Status metric
		if v.CallInfo.Status != "" {
			d.emitMetric(ctx, v.ContextID, []*protos.Metric{
				{Name: obs.MetricCallStatus, Value: v.CallInfo.Status, Description: "Call status"},
			})
		}

		// Provider event
		if v.CallInfo.StatusInfo.Event != "" {
			d.emitEvent(ctx, v.ContextID, obs.ComponentTelephony, map[string]string{
				obs.DataType:     v.CallInfo.StatusInfo.Event,
				obs.DataProvider: v.Provider,
				obs.DataCaller:   v.CallInfo.CallerNumber,
			})
		}
	}

	d.emitEvent(ctx, v.ContextID, obs.ComponentTelephony, map[string]string{
		obs.DataType:      obs.EventCallReceived,
		obs.DataProvider:  v.Provider,
		obs.DataContextID: v.ContextID,
		"conversation_id": fmt.Sprintf("%d", v.ConversationID),
	})

	d.OnPipeline(ctx, ProviderAnsweringPipeline{
		ID:             v.ContextID,
		Provider:       v.Provider,
		Auth:           v.Auth,
		AssistantID:    v.AssistantID,
		ConversationID: v.ConversationID,
		ContextID:      v.ContextID,
		CallerNumber:   v.CallInfo.CallerNumber,
		GinContext:     v.GinContext,
	})
}

func (d *Dispatcher) handleProviderAnswering(ctx context.Context, v ProviderAnsweringPipeline) {
	d.logger.Infow("Pipeline: ProviderAnswering", "context_id", v.ContextID)

	if d.onAnswerProvider != nil {
		if err := d.onAnswerProvider(ctx, v.GinContext, v.Auth, v.Provider, v.AssistantID, v.CallerNumber, v.ConversationID); err != nil {
			d.failWithResult(v.ContextID, "provider_answer", err)
			return
		}
	}

	// Send result to controller (unblocks CallReciever HTTP handler)
	d.sendStoredResult(v.ContextID, &PipelineResult{
		ContextID:      v.ContextID,
		ConversationID: v.ConversationID,
	})

	d.OnPipeline(ctx, ProviderAnsweredPipeline{ID: v.ContextID, ContextID: v.ContextID})
}

func (d *Dispatcher) handleProviderAnswered(ctx context.Context, v ProviderAnsweredPipeline) {
	d.logger.Infow("Pipeline: ProviderAnswered", "context_id", v.ContextID)
	d.emitEvent(ctx, v.ID, obs.ComponentTelephony, map[string]string{
		obs.DataType:      obs.EventProviderAnswered,
		obs.DataContextID: v.ContextID,
	})
}

// resultChStore stores a result channel for a call (forwarded across stages).
func (d *Dispatcher) resultChStore(callID string, ch chan<- *PipelineResult) {
	d.mu.Lock()
	if d.pendingResults == nil {
		d.pendingResults = make(map[string]chan<- *PipelineResult)
	}
	d.pendingResults[callID] = ch
	d.mu.Unlock()
}

// sendStoredResult sends to the stored result channel and removes it.
func (d *Dispatcher) sendStoredResult(callID string, r *PipelineResult) {
	d.mu.Lock()
	ch, ok := d.pendingResults[callID]
	if ok {
		delete(d.pendingResults, callID)
	}
	d.mu.Unlock()
	if ok && ch != nil {
		ch <- r
	}
}

// failWithResult sends a failure to the stored result channel and emits CallFailed.
func (d *Dispatcher) failWithResult(callID, stage string, err error) {
	d.sendStoredResult(callID, &PipelineResult{Error: err})
	d.OnPipeline(context.Background(), CallFailedPipeline{ID: callID, Stage: stage, Error: err})
}
