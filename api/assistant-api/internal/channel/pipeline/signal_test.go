// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	obs "github.com/rapidaai/api/assistant-api/internal/observe"
	"github.com/rapidaai/pkg/commons"
)

func newSignalTestLogger() commons.Logger {
	l, _ := commons.NewApplicationLogger(commons.Level("error"), commons.Name("signal-test"))
	return l
}

// testHooks tracks which lifecycle methods were called on ConversationHooks.
type testHooks struct {
	hooks    *obs.ConversationHooks
	onEndCh  chan struct{}
	onErrCh  chan struct{}
}

func newTestHooks() *testHooks {
	th := &testHooks{
		onEndCh: make(chan struct{}, 1),
		onErrCh: make(chan struct{}, 1),
	}
	// Create real hooks with an empty snapshot and no-op dependencies.
	// OnEnd and OnError fire webhooks on the snapshot; with no webhooks configured they are no-ops.
	th.hooks = obs.NewConversationHooks(&obs.ConversationHooksConfig{
		Logger: newSignalTestLogger(),
		Snapshot: &obs.ConversationSnapshot{
			Assistant:    &internal_assistant_entity.Assistant{},
			Conversation: &obs.ConversationRef{ID: 1},
		},
	})
	return th
}

func newSignalDispatcher() *Dispatcher {
	return NewDispatcher(&DispatcherConfig{Logger: newSignalTestLogger()})
}

func TestHandleCallCompleted_RemovesHooksAndObserver(t *testing.T) {
	d := newSignalDispatcher()
	ctx := context.Background()
	d.Start(ctx)

	callID := "completed-call"

	// Store a mock observer
	o := obs.NewConversationObserver(&obs.ConversationObserverConfig{
		Logger: newSignalTestLogger(),
	})
	d.storeObserver(callID, o)

	// Store hooks
	th := newTestHooks()
	d.storeHooks(callID, th.hooks)

	d.OnPipeline(ctx, CallCompletedPipeline{
		ID:       callID,
		Duration: 5 * time.Second,
		Messages: 10,
		Reason:   "caller_hangup",
	})

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Hooks should be removed
	if _, ok := d.getHooks(callID); ok {
		t.Error("hooks should have been removed after CallCompleted")
	}

	// Observer should be removed
	if _, ok := d.getObserver(callID); ok {
		t.Error("observer should have been removed after CallCompleted")
	}
}

func TestHandleCallFailed_RemovesHooksAndObserver(t *testing.T) {
	d := newSignalDispatcher()
	ctx := context.Background()
	d.Start(ctx)

	callID := "failed-call"

	o := obs.NewConversationObserver(&obs.ConversationObserverConfig{
		Logger: newSignalTestLogger(),
	})
	d.storeObserver(callID, o)

	th := newTestHooks()
	d.storeHooks(callID, th.hooks)

	d.OnPipeline(ctx, CallFailedPipeline{
		ID:    callID,
		Stage: "streamer_create",
		Error: context.DeadlineExceeded,
	})

	time.Sleep(100 * time.Millisecond)

	if _, ok := d.getHooks(callID); ok {
		t.Error("hooks should have been removed after CallFailed")
	}

	if _, ok := d.getObserver(callID); ok {
		t.Error("observer should have been removed after CallFailed")
	}
}

func TestHandleCallCompleted_NoHooks_NoObserver(t *testing.T) {
	d := newSignalDispatcher()
	ctx := context.Background()
	d.Start(ctx)

	// Should not panic when no hooks/observer are stored
	d.OnPipeline(ctx, CallCompletedPipeline{
		ID:       "orphan-call",
		Duration: 1 * time.Second,
		Reason:   "normal",
	})

	time.Sleep(50 * time.Millisecond)
}

func TestHandleCallFailed_NoHooks_NoObserver(t *testing.T) {
	d := newSignalDispatcher()
	ctx := context.Background()
	d.Start(ctx)

	d.OnPipeline(ctx, CallFailedPipeline{
		ID:    "orphan-fail",
		Stage: "vault",
		Error: context.Canceled,
	})

	time.Sleep(50 * time.Millisecond)
}

func TestHandleDisconnectRequested_DoesNotRemoveObserver(t *testing.T) {
	d := newSignalDispatcher()
	ctx := context.Background()
	d.Start(ctx)

	callID := "disconnect-call"

	o := obs.NewConversationObserver(&obs.ConversationObserverConfig{
		Logger: newSignalTestLogger(),
	})
	d.storeObserver(callID, o)

	d.OnPipeline(ctx, DisconnectRequestedPipeline{
		ID:     callID,
		Reason: "user_request",
	})

	time.Sleep(50 * time.Millisecond)

	// Observer should still exist (disconnect requested is not terminal)
	if _, ok := d.getObserver(callID); !ok {
		t.Error("observer should still exist after DisconnectRequested")
	}
}

func TestSignalHandlers_ConcurrentCalls(t *testing.T) {
	d := newSignalDispatcher()
	ctx := context.Background()
	d.Start(ctx)

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		callID := "concurrent-" + time.Now().Format("150405.000000") + "-" + string(rune('A'+i))
		o := obs.NewConversationObserver(&obs.ConversationObserverConfig{
			Logger: newSignalTestLogger(),
		})
		d.storeObserver(callID, o)

		th := newTestHooks()
		d.storeHooks(callID, th.hooks)

		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if id[len(id)-1]%2 == 0 {
				d.OnPipeline(ctx, CallCompletedPipeline{ID: id, Duration: time.Second, Reason: "normal"})
			} else {
				d.OnPipeline(ctx, CallFailedPipeline{ID: id, Stage: "test", Error: context.Canceled})
			}
		}(callID)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)
}
