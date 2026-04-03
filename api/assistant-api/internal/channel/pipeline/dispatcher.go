// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_pipeline

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	callcontext "github.com/rapidaai/api/assistant-api/internal/callcontext"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/types"
	"github.com/rapidaai/protos"
)

const (
	signalChSize  = 64
	setupChSize   = 256
	mediaChSize   = 256
	controlChSize = 512
)

// PipelineResult carries the outcome of a sync pipeline stage back to the caller.
type PipelineResult struct {
	ContextID      string
	ConversationID uint64
	Error          error

	Provider     string
	CallerNumber string
	CallStatus   string
	CallEvent    string
	Extra        map[string]string // provider-specific metadata
}

type callEnvelope struct {
	ctx      context.Context
	p        Pipeline
	resultCh chan<- *PipelineResult // nil for async (fire-and-forget)
}

// Dispatcher routes channel call lifecycle stages to priority-based goroutines.
//
//	signal  — disconnect, completed, failed
//	setup   — call received, conversation created
//	media   — session connected, initialized, active
//	control — events, metrics
type Dispatcher struct {
	mu     sync.RWMutex
	logger commons.Logger

	signalCh  chan callEnvelope
	setupCh   chan callEnvelope
	mediaCh   chan callEnvelope
	controlCh chan callEnvelope

	observers      map[string]*observe.ConversationObserver
	hooks          map[string]*observe.ConversationHooks
	pendingResults map[string]chan<- *PipelineResult

	// Callbacks — each stage has its own independent callback
	onReceiveCall        OnReceiveCallFunc
	onLoadAssistant      OnLoadAssistantFunc
	onCreateConversation OnCreateConversationFunc
	onSaveCallContext    OnSaveCallContextFunc
	onAnswerProvider          OnAnswerProviderFunc
	onDispatchOutbound        OnDispatchOutboundFunc
	onApplyConversationExtras OnApplyConversationExtrasFunc
	onResolveSession          OnResolveSessionFunc
	onCreateStreamer     OnCreateStreamerFunc
	onCreateTalker       OnCreateTalkerFunc
	onRunTalk            OnRunTalkFunc
	onCreateObserver     OnCreateObserverFunc
	onCreateHooks        OnCreateHooksFunc
	onCompleteSession    OnCompleteSessionFunc
}

// OnReceiveCallFunc parses the provider webhook and returns CallInfo.
type OnReceiveCallFunc func(ctx context.Context, provider string, ginCtx *gin.Context) (*internal_type.CallInfo, error)

// OnLoadAssistantFunc loads the assistant from DB.
type OnLoadAssistantFunc func(ctx context.Context, auth types.SimplePrinciple, assistantID uint64) (*internal_assistant_entity.Assistant, error)

// OnCreateConversationFunc creates a conversation and returns conversationID.
type OnCreateConversationFunc func(ctx context.Context, auth types.SimplePrinciple, callerNumber string, assistantID, assistantProviderID uint64, direction string) (conversationID uint64, err error)

// OnSaveCallContextFunc saves the call context to Postgres and returns contextID.
type OnSaveCallContextFunc func(ctx context.Context, auth types.SimplePrinciple, assistant *internal_assistant_entity.Assistant, conversationID uint64, callInfo *internal_type.CallInfo, provider string) (contextID string, err error)

// OnAnswerProviderFunc instructs the provider to answer the call.
type OnAnswerProviderFunc func(ctx context.Context, ginCtx *gin.Context, auth types.SimplePrinciple, provider string, assistantID uint64, callerNumber string, conversationID uint64) error

// OnDispatchOutboundFunc dispatches the outbound call (claim, vault, dial).
type OnDispatchOutboundFunc func(ctx context.Context, contextID string) error

// OnApplyConversationExtrasFunc applies options/arguments/metadata to a conversation.
type OnApplyConversationExtrasFunc func(ctx context.Context, auth types.SimplePrinciple, assistantID, conversationID uint64, opts, args, metadata map[string]interface{}) error

// OnResolveSessionFunc resolves a call context and vault credential from a contextID.
type OnResolveSessionFunc func(ctx context.Context, contextID string) (*callcontext.CallContext, *protos.VaultCredential, error)

// OnCreateStreamerFunc creates a provider-specific streamer.
type OnCreateStreamerFunc func(ctx context.Context, cc *callcontext.CallContext, vc *protos.VaultCredential, ws *websocket.Conn, conn net.Conn, reader *bufio.Reader, writer *bufio.Writer) (internal_type.Streamer, error)

// OnCreateTalkerFunc creates a talker (genericRequestor).
type OnCreateTalkerFunc func(ctx context.Context, streamer internal_type.Streamer) (internal_type.Talking, error)

// OnRunTalkFunc runs talker.Talk (blocking for call duration).
type OnRunTalkFunc func(ctx context.Context, talker internal_type.Talking, auth types.SimplePrinciple) error

// OnCreateObserverFunc creates a ConversationObserver.
type OnCreateObserverFunc func(ctx context.Context, callID string, auth types.SimplePrinciple, assistantID, conversationID uint64) *observe.ConversationObserver

// OnCreateHooksFunc creates ConversationHooks (webhooks + analysis).
type OnCreateHooksFunc func(ctx context.Context, auth types.SimplePrinciple, assistantID, conversationID uint64) *observe.ConversationHooks

// OnCompleteSessionFunc marks a call context as completed.
type OnCompleteSessionFunc func(ctx context.Context, contextID string)

// DispatcherConfig holds dependencies for creating a channel dispatcher.
type DispatcherConfig struct {
	Logger               commons.Logger
	OnReceiveCall        OnReceiveCallFunc
	OnLoadAssistant      OnLoadAssistantFunc
	OnCreateConversation OnCreateConversationFunc
	OnSaveCallContext    OnSaveCallContextFunc
	OnAnswerProvider          OnAnswerProviderFunc
	OnDispatchOutbound        OnDispatchOutboundFunc
	OnApplyConversationExtras OnApplyConversationExtrasFunc
	OnResolveSession          OnResolveSessionFunc
	OnCreateStreamer     OnCreateStreamerFunc
	OnCreateTalker       OnCreateTalkerFunc
	OnRunTalk            OnRunTalkFunc
	OnCreateObserver     OnCreateObserverFunc
	OnCreateHooks        OnCreateHooksFunc
	OnCompleteSession    OnCompleteSessionFunc
}

func NewDispatcher(cfg *DispatcherConfig) *Dispatcher {
	return &Dispatcher{
		logger:               cfg.Logger,
		observers:      make(map[string]*observe.ConversationObserver),
		hooks:          make(map[string]*observe.ConversationHooks),
		pendingResults: make(map[string]chan<- *PipelineResult),
		onReceiveCall:        cfg.OnReceiveCall,
		onLoadAssistant:      cfg.OnLoadAssistant,
		onCreateConversation: cfg.OnCreateConversation,
		onSaveCallContext:    cfg.OnSaveCallContext,
		onAnswerProvider:          cfg.OnAnswerProvider,
		onDispatchOutbound:        cfg.OnDispatchOutbound,
		onApplyConversationExtras: cfg.OnApplyConversationExtras,
		onResolveSession:          cfg.OnResolveSession,
		onCreateStreamer:     cfg.OnCreateStreamer,
		onCreateTalker:       cfg.OnCreateTalker,
		onRunTalk:            cfg.OnRunTalk,
		onCreateObserver:     cfg.OnCreateObserver,
		onCreateHooks:        cfg.OnCreateHooks,
		onCompleteSession:    cfg.OnCompleteSession,
		signalCh:             make(chan callEnvelope, signalChSize),
		setupCh:              make(chan callEnvelope, setupChSize),
		mediaCh:              make(chan callEnvelope, mediaChSize),
		controlCh:            make(chan callEnvelope, controlChSize),
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	go d.runDispatcher(ctx, d.signalCh)
	go d.runDispatcher(ctx, d.setupCh)
	go d.runDispatcher(ctx, d.mediaCh)
	go d.runDispatcher(ctx, d.controlCh)
	d.logger.Infow("Channel pipeline dispatcher started")
}

// OnPipeline enqueues a pipeline stage asynchronously (fire-and-forget).
func (d *Dispatcher) OnPipeline(ctx context.Context, stages ...Pipeline) {
	for _, s := range stages {
		d.enqueue(ctx, s, nil)
	}
}

// RunSync enqueues a pipeline stage and blocks until the handler completes.
// The handler writes its result to the result channel. The controller blocks here.
func (d *Dispatcher) RunSync(ctx context.Context, stage Pipeline) *PipelineResult {
	resultCh := make(chan *PipelineResult, 1)
	d.enqueue(ctx, stage, resultCh)
	select {
	case r := <-resultCh:
		return r
	case <-ctx.Done():
		return &PipelineResult{Error: ctx.Err()}
	}
}

func (d *Dispatcher) enqueue(ctx context.Context, s Pipeline, resultCh chan<- *PipelineResult) {
	e := callEnvelope{ctx: ctx, p: s, resultCh: resultCh}
	switch s.(type) {
	case DisconnectRequestedPipeline, CallCompletedPipeline, CallFailedPipeline:
		d.signalCh <- e
	case CallReceivedPipeline, WebhookParsedPipeline, AssistantResolvedPipeline,
		ConversationCreatedPipeline, ProviderAnsweringPipeline, ProviderAnsweredPipeline,
		OutboundRequestedPipeline, OutboundDialedPipeline:
		d.setupCh <- e
	case SessionConnectedPipeline, SessionInitializedPipeline, CallActivePipeline, ModeSwitchPipeline:
		d.mediaCh <- e
	case EventEmittedPipeline, MetricEmittedPipeline:
		d.controlCh <- e
	default:
		d.logger.Warnw("OnPipeline: unrouted type", "type", fmt.Sprintf("%T", s))
		d.setupCh <- e
	}
}

func (d *Dispatcher) runDispatcher(ctx context.Context, ch chan callEnvelope) {
	for {
		select {
		case <-ctx.Done():
			d.drain(ch)
			return
		case e := <-ch:
			d.dispatch(e)
		}
	}
}

func (d *Dispatcher) drain(ch chan callEnvelope) {
	for {
		select {
		case e := <-ch:
			d.dispatch(e)
		default:
			return
		}
	}
}

func (d *Dispatcher) dispatch(e callEnvelope) {
	ctx := e.ctx
	resultCh := e.resultCh

	switch v := e.p.(type) {
	case CallReceivedPipeline:
		d.handleCallReceived(ctx, v, resultCh)
	case SessionConnectedPipeline:
		d.handleSessionConnected(ctx, v, resultCh)

	case WebhookParsedPipeline:
		d.handleWebhookParsed(ctx, v)
	case AssistantResolvedPipeline:
		d.handleAssistantResolved(ctx, v)
	case ConversationCreatedPipeline:
		d.handleConversationCreated(ctx, v)
	case ProviderAnsweringPipeline:
		d.handleProviderAnswering(ctx, v)
	case ProviderAnsweredPipeline:
		d.handleProviderAnswered(ctx, v)
	case SessionInitializedPipeline:
		d.handleSessionInitialized(ctx, v)
	case CallActivePipeline:
		d.handleCallActive(ctx, v)
	case ModeSwitchPipeline:
		d.handleModeSwitch(ctx, v)
	case DisconnectRequestedPipeline:
		d.handleDisconnectRequested(ctx, v)
	case CallCompletedPipeline:
		d.handleCallCompleted(ctx, v)
	case CallFailedPipeline:
		d.handleCallFailed(ctx, v)
	case OutboundRequestedPipeline:
		d.handleOutboundRequested(ctx, v, resultCh)
	case OutboundDialedPipeline:
		d.handleOutboundDialed(ctx, v)
	case EventEmittedPipeline:
		d.handleEventEmitted(ctx, v)
	case MetricEmittedPipeline:
		d.handleMetricEmitted(ctx, v)
	default:
		d.logger.Warnw("dispatch: unknown pipeline type", "type", fmt.Sprintf("%T", e.p))
	}
}

func (d *Dispatcher) storeObserver(callID string, obs *observe.ConversationObserver) {
	d.mu.Lock()
	d.observers[callID] = obs
	d.mu.Unlock()
}

func (d *Dispatcher) getObserver(callID string) (*observe.ConversationObserver, bool) {
	d.mu.RLock()
	obs, ok := d.observers[callID]
	d.mu.RUnlock()
	return obs, ok
}

func (d *Dispatcher) removeObserver(ctx context.Context, callID string) {
	d.mu.Lock()
	obs, ok := d.observers[callID]
	if ok {
		delete(d.observers, callID)
	}
	d.mu.Unlock()
	if ok && obs != nil {
		obs.Shutdown(ctx)
	}
}

func (d *Dispatcher) emitEvent(ctx context.Context, callID, name string, data map[string]string) {
	obs, ok := d.getObserver(callID)
	if !ok {
		return
	}
	obs.EmitEvent(ctx, name, data)
}

func (d *Dispatcher) emitMetric(ctx context.Context, callID string, metrics []*protos.Metric) {
	if len(metrics) == 0 {
		return
	}
	obs, ok := d.getObserver(callID)
	if !ok {
		return
	}
	obs.EmitMetric(ctx, metrics)
}

func (d *Dispatcher) storeHooks(callID string, h *observe.ConversationHooks) {
	d.mu.Lock()
	d.hooks[callID] = h
	d.mu.Unlock()
}

func (d *Dispatcher) getHooks(callID string) (*observe.ConversationHooks, bool) {
	d.mu.RLock()
	h, ok := d.hooks[callID]
	d.mu.RUnlock()
	return h, ok
}

func (d *Dispatcher) removeHooks(callID string) {
	d.mu.Lock()
	delete(d.hooks, callID)
	d.mu.Unlock()
}

// sendResult is a nil-safe helper to send to resultCh.
func sendResult(ch chan<- *PipelineResult, r *PipelineResult) {
	if ch != nil {
		ch <- r
	}
}
