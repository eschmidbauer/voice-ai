// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"context"
	"fmt"
	"sync"

	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
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

type callEnvelope struct {
	ctx context.Context
	p   sip_infra.Pipeline
}

// Dispatcher routes SIP pipeline stages to priority-based channel goroutines.
// Priority: signal > setup > media > control.
type Dispatcher struct {
	mu     sync.RWMutex
	logger commons.Logger

	signalCh  chan callEnvelope
	setupCh   chan callEnvelope
	mediaCh   chan callEnvelope
	controlCh chan callEnvelope

	observers map[string]*observe.ConversationObserver
	hooks     map[string]*observe.ConversationHooks

	server             *sip_infra.Server
	registrationClient *sip_infra.RegistrationClient

	didResolver      DIDResolverFunc
	onCallSetup      OnCallSetupFunc
	onCallStart      OnCallStartFunc
	onCallEnd        OnCallEndFunc
	onCreateObserver OnCreateObserverFunc
	onCreateHooks    OnCreateHooksFunc
}

type DIDResolverFunc func(did string) (assistantID uint64, auth types.SimplePrinciple, err error)

type OnCallSetupFunc func(ctx context.Context, session *sip_infra.Session, auth types.SimplePrinciple, assistantID uint64, fromURI string, direction string) (*CallSetupResult, error)

// CallSetupResult carries the output of OnCallSetupFunc into the pipeline.
type CallSetupResult struct {
	AssistantID         uint64
	ConversationID      uint64
	AssistantProviderId uint64
	AuthToken           string
	AuthType            string
	ProjectID           uint64
	OrganizationID      uint64
}

type OnCallStartFunc func(ctx context.Context, session *sip_infra.Session, setup *CallSetupResult, vaultCred interface{}, sipConfig *sip_infra.Config, direction string)

type OnCallEndFunc func(callID string)

type OnCreateObserverFunc func(ctx context.Context, setup *CallSetupResult, auth types.SimplePrinciple) *observe.ConversationObserver

type OnCreateHooksFunc func(ctx context.Context, auth types.SimplePrinciple, assistantID, conversationID uint64) *observe.ConversationHooks

type DispatcherConfig struct {
	Logger             commons.Logger
	Server             *sip_infra.Server
	RegistrationClient *sip_infra.RegistrationClient
	DIDResolver        DIDResolverFunc
	OnCallSetup        OnCallSetupFunc
	OnCallStart        OnCallStartFunc
	OnCallEnd          OnCallEndFunc
	OnCreateObserver   OnCreateObserverFunc
	OnCreateHooks      OnCreateHooksFunc
}

// NewDispatcher creates a SIP call pipeline dispatcher.
func NewDispatcher(cfg *DispatcherConfig) *Dispatcher {
	return &Dispatcher{
		logger:             cfg.Logger,
		server:             cfg.Server,
		registrationClient: cfg.RegistrationClient,
		observers:          make(map[string]*observe.ConversationObserver),
		hooks:              make(map[string]*observe.ConversationHooks),
		didResolver:        cfg.DIDResolver,
		onCallSetup:        cfg.OnCallSetup,
		onCallStart:        cfg.OnCallStart,
		onCallEnd:          cfg.OnCallEnd,
		onCreateObserver:   cfg.OnCreateObserver,
		onCreateHooks:      cfg.OnCreateHooks,
		signalCh:           make(chan callEnvelope, signalChSize),
		setupCh:            make(chan callEnvelope, setupChSize),
		mediaCh:            make(chan callEnvelope, mediaChSize),
		controlCh:          make(chan callEnvelope, controlChSize),
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

func (d *Dispatcher) Start(ctx context.Context) {
	go d.runSignalDispatcher(ctx)
	go d.runSetupDispatcher(ctx)
	go d.runMediaDispatcher(ctx)
	go d.runControlDispatcher(ctx)

	d.logger.Infow("SIP pipeline dispatcher started")
}

func (d *Dispatcher) OnPipeline(ctx context.Context, stages ...sip_infra.Pipeline) {
	for _, s := range stages {
		e := callEnvelope{ctx: ctx, p: s}
		switch s.(type) {
		case sip_infra.ByeReceivedPipeline,
			sip_infra.CancelReceivedPipeline,
			sip_infra.TransferRequestedPipeline,
			sip_infra.CallEndedPipeline,
			sip_infra.CallFailedPipeline:
			d.signalCh <- e

		case sip_infra.InviteReceivedPipeline,
			sip_infra.RouteResolvedPipeline,
			sip_infra.AuthenticatedPipeline,
			sip_infra.OutboundRequestedPipeline,
			sip_infra.InviteSentPipeline,
			sip_infra.AnswerReceivedPipeline:
			d.setupCh <- e

		case sip_infra.SessionEstablishedPipeline,
			sip_infra.CallStartedPipeline,
			sip_infra.HoldRequestedPipeline,
			sip_infra.ReInviteReceivedPipeline:
			d.mediaCh <- e

		case sip_infra.EventEmittedPipeline,
			sip_infra.MetricEmittedPipeline,
			sip_infra.RecordingStartedPipeline,
			sip_infra.DTMFReceivedPipeline,
			sip_infra.RegisterRequestedPipeline,
			sip_infra.RegisterActivePipeline,
			sip_infra.RegisterFailedPipeline,
			sip_infra.RegisterExpiringPipeline:
			d.controlCh <- e

		default:
			d.logger.Warnw("OnPipeline: unrouted pipeline type, falling back to setupCh", "type", fmt.Sprintf("%T", s))
			d.setupCh <- e
		}
	}
}

func (d *Dispatcher) runSignalDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drain(d.signalCh)
			return
		case e := <-d.signalCh:
			d.dispatch(e.ctx, e.p)
		}
	}
}

func (d *Dispatcher) runSetupDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drain(d.setupCh)
			return
		case e := <-d.setupCh:
			d.dispatch(e.ctx, e.p)
		}
	}
}

func (d *Dispatcher) runMediaDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drain(d.mediaCh)
			return
		case e := <-d.mediaCh:
			d.dispatch(e.ctx, e.p)
		}
	}
}

func (d *Dispatcher) runControlDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drain(d.controlCh)
			return
		case e := <-d.controlCh:
			d.dispatch(e.ctx, e.p)
		}
	}
}

func (d *Dispatcher) drain(ch chan callEnvelope) {
	for {
		select {
		case e := <-ch:
			d.dispatch(e.ctx, e.p)
		default:
			return
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context, p sip_infra.Pipeline) {
	switch v := p.(type) {
	case sip_infra.InviteReceivedPipeline:
		d.handleInviteReceived(ctx, v)
	case sip_infra.RouteResolvedPipeline:
		d.handleRouteResolved(ctx, v)
	case sip_infra.AuthenticatedPipeline:
		d.handleAuthenticated(ctx, v)
	case sip_infra.OutboundRequestedPipeline:
		d.handleOutboundRequested(ctx, v)
	case sip_infra.InviteSentPipeline:
		d.handleInviteSent(ctx, v)
	case sip_infra.AnswerReceivedPipeline:
		d.handleAnswerReceived(ctx, v)

	case sip_infra.SessionEstablishedPipeline:
		d.handleSessionEstablished(ctx, v)
	case sip_infra.CallStartedPipeline:
		d.handleCallStarted(ctx, v)
	case sip_infra.HoldRequestedPipeline:
		d.handleHoldRequested(ctx, v)
	case sip_infra.ReInviteReceivedPipeline:
		d.handleReInviteReceived(ctx, v)

	case sip_infra.ByeReceivedPipeline:
		d.handleByeReceived(ctx, v)
	case sip_infra.CancelReceivedPipeline:
		d.handleCancelReceived(ctx, v)
	case sip_infra.TransferRequestedPipeline:
		d.handleTransferRequested(ctx, v)
	case sip_infra.CallEndedPipeline:
		d.handleCallEnded(ctx, v)
	case sip_infra.CallFailedPipeline:
		d.handleCallFailed(ctx, v)

	case sip_infra.EventEmittedPipeline:
		d.handleEventEmitted(ctx, v)
	case sip_infra.MetricEmittedPipeline:
		d.handleMetricEmitted(ctx, v)
	case sip_infra.RecordingStartedPipeline:
		d.handleRecordingStarted(ctx, v)
	case sip_infra.DTMFReceivedPipeline:
		d.handleDTMFReceived(ctx, v)
	case sip_infra.RegisterRequestedPipeline:
		d.handleRegisterRequested(ctx, v)
	case sip_infra.RegisterActivePipeline:
		d.handleRegisterActive(ctx, v)
	case sip_infra.RegisterFailedPipeline:
		d.handleRegisterFailed(ctx, v)
	case sip_infra.RegisterExpiringPipeline:
		d.handleRegisterExpiring(ctx, v)

	default:
		d.logger.Warnw("dispatch: unknown pipeline type", "type", fmt.Sprintf("%T", p))
	}
}
