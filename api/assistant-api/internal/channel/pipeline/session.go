// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_pipeline

import (
	"context"

	obs "github.com/rapidaai/api/assistant-api/internal/observe"
)

// handleSessionConnected resolves context, creates streamer/talker, runs Talk (blocking), then cleans up.
func (d *Dispatcher) handleSessionConnected(ctx context.Context, v SessionConnectedPipeline, resultCh chan<- *PipelineResult) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				d.logger.Error("Pipeline: SessionConnected panicked", "call_id", v.ID, "panic", r)
				sendResult(resultCh, &PipelineResult{Error: ErrCallbackNotConfigured})
			}
		}()

		d.logger.Infow("Pipeline: SessionConnected", "call_id", v.ID)

		if d.onResolveSession == nil {
			sendResult(resultCh, &PipelineResult{Error: ErrCallbackNotConfigured})
			return
		}
		cc, vc, err := d.onResolveSession(ctx, v.ContextID)
		if err != nil {
			d.logger.Error("Pipeline: session resolution failed", "call_id", v.ID, "error", err)
			sendResult(resultCh, &PipelineResult{Error: err})
			return
		}

		if d.onCreateStreamer == nil {
			sendResult(resultCh, &PipelineResult{Error: ErrCallbackNotConfigured})
			return
		}
		streamer, err := d.onCreateStreamer(ctx, cc, vc, v.WebSocket, v.Conn, v.Reader, v.Writer)
		if err != nil {
			d.logger.Error("Pipeline: streamer creation failed", "call_id", v.ID, "error", err)
			sendResult(resultCh, &PipelineResult{Error: err})
			return
		}

		if d.onCreateTalker == nil {
			sendResult(resultCh, &PipelineResult{Error: ErrCallbackNotConfigured})
			return
		}
		talker, err := d.onCreateTalker(ctx, streamer)
		if err != nil {
			d.logger.Error("Pipeline: talker creation failed", "call_id", v.ID, "error", err)
			sendResult(resultCh, &PipelineResult{Error: err})
			return
		}

		auth := cc.ToAuth()
		contextID := v.ContextID
		if contextID == "" {
			contextID = v.ID
		}

		if _, exists := d.getObserver(contextID); !exists && d.onCreateObserver != nil {
			o := d.onCreateObserver(ctx, contextID, auth, cc.AssistantID, cc.ConversationID)
			if o != nil {
				d.storeObserver(contextID, o)
			}
		}

		if d.onCreateHooks != nil {
			hooks := d.onCreateHooks(ctx, auth, cc.AssistantID, cc.ConversationID)
			if hooks != nil {
				d.storeHooks(contextID, hooks)
				hooks.OnBegin(ctx)
			}
		}

		d.emitEvent(ctx, contextID, obs.ComponentSession, map[string]string{
			obs.DataType:     obs.EventSessionConnected,
			obs.DataProvider: cc.Provider,
		})

		if d.onRunTalk == nil {
			sendResult(resultCh, &PipelineResult{Error: ErrCallbackNotConfigured})
			return
		}
		talkErr := d.onRunTalk(ctx, talker, auth)

		if hooks, ok := d.getHooks(contextID); ok {
			hooks.OnEnd(ctx)
			d.removeHooks(contextID)
		}

		d.emitEvent(ctx, contextID, obs.ComponentSession, map[string]string{
			obs.DataType:   obs.EventCallCompleted,
			obs.DataReason: "talk_completed",
		})

		d.removeObserver(ctx, contextID)

		if d.onCompleteSession != nil {
			d.onCompleteSession(ctx, contextID)
		}

		sendResult(resultCh, &PipelineResult{Error: talkErr})
	}()
}

func (d *Dispatcher) handleSessionInitialized(ctx context.Context, v SessionInitializedPipeline) {
	d.logger.Infow("Pipeline: SessionInitialized", "call_id", v.ID)
}

func (d *Dispatcher) handleCallActive(ctx context.Context, v CallActivePipeline) {
	d.logger.Infow("Pipeline: CallActive", "call_id", v.ID)
}

func (d *Dispatcher) handleModeSwitch(ctx context.Context, v ModeSwitchPipeline) {
	d.logger.Infow("Pipeline: ModeSwitch", "call_id", v.ID, "from", v.From, "to", v.To)
	d.emitEvent(ctx, v.ID, obs.ComponentSession, map[string]string{
		obs.DataType: obs.EventModeSwitch,
		obs.DataFrom: v.From,
		obs.DataTo:   v.To,
	})
}
