// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_pipeline

import (
	"context"
	"fmt"

	observe "github.com/rapidaai/api/assistant-api/internal/observe"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/types"
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
// Stateless — no per-call state stored on the Dispatcher.
type Dispatcher struct {
	logger commons.Logger

	signalCh  chan callEnvelope
	setupCh   chan callEnvelope
	mediaCh   chan callEnvelope
	controlCh chan callEnvelope

	server             *sip_infra.Server
	registrationClient *sip_infra.RegistrationClient

	didResolver          DIDResolverFunc
	onCreateConversation OnCreateConversationFunc
	onCallSetup          OnCallSetupFunc
	onCallStart          OnCallStartFunc
	onCallEnd            OnCallEndFunc
	onCreateObserver     OnCreateObserverFunc
	onCreateHooks        OnCreateHooksFunc
}

type DIDResolverFunc func(did string) (assistantID uint64, auth types.SimplePrinciple, err error)

type OnCreateConversationFunc func(ctx context.Context, auth types.SimplePrinciple, assistantID uint64, fromURI string, direction string) (conversationID uint64, err error)

type OnCallSetupFunc func(ctx context.Context, session *sip_infra.Session, auth types.SimplePrinciple, assistantID uint64, conversationID uint64) (*CallSetupResult, error)

type CallSetupResult struct {
	AssistantID         uint64
	ConversationID      uint64
	AssistantProviderId uint64
	AuthToken           string
	AuthType            string
	ProjectID           uint64
	OrganizationID      uint64
}

type OnCallStartFunc func(ctx context.Context, session *sip_infra.Session, setup *CallSetupResult, vaultCred interface{}, sipConfig *sip_infra.Config, direction string) error

type OnCallEndFunc func(callID string)

type OnCreateObserverFunc func(ctx context.Context, setup *CallSetupResult, auth types.SimplePrinciple) *observe.ConversationObserver

type OnCreateHooksFunc func(ctx context.Context, auth types.SimplePrinciple, assistantID, conversationID uint64) *observe.ConversationHooks

type DispatcherConfig struct {
	Logger               commons.Logger
	Server               *sip_infra.Server
	RegistrationClient   *sip_infra.RegistrationClient
	DIDResolver          DIDResolverFunc
	OnCreateConversation OnCreateConversationFunc
	OnCallSetup          OnCallSetupFunc
	OnCallStart          OnCallStartFunc
	OnCallEnd            OnCallEndFunc
	OnCreateObserver     OnCreateObserverFunc
	OnCreateHooks        OnCreateHooksFunc
}

func NewDispatcher(cfg *DispatcherConfig) *Dispatcher {
	return &Dispatcher{
		logger:               cfg.Logger,
		server:               cfg.Server,
		registrationClient:   cfg.RegistrationClient,
		didResolver:          cfg.DIDResolver,
		onCreateConversation: cfg.OnCreateConversation,
		onCallSetup:          cfg.OnCallSetup,
		onCallStart:          cfg.OnCallStart,
		onCallEnd:            cfg.OnCallEnd,
		onCreateObserver:     cfg.OnCreateObserver,
		onCreateHooks:        cfg.OnCreateHooks,
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
	d.logger.Infow("SIP pipeline dispatcher started")
}

func (d *Dispatcher) OnPipeline(ctx context.Context, stages ...sip_infra.Pipeline) {
	for _, s := range stages {
		e := callEnvelope{ctx: ctx, p: s}
		switch s.(type) {
		case sip_infra.ByeReceivedPipeline,
			sip_infra.CancelReceivedPipeline,
			sip_infra.TransferInitiatedPipeline,
			sip_infra.TransferConnectedPipeline,
			sip_infra.TransferFailedPipeline,
			sip_infra.CallEndedPipeline,
			sip_infra.CallFailedPipeline:
			d.signalCh <- e
		case sip_infra.SessionEstablishedPipeline:
			d.mediaCh <- e
		case sip_infra.EventEmittedPipeline,
			sip_infra.MetricEmittedPipeline,
			sip_infra.DTMFReceivedPipeline,
			sip_infra.RegisterRequestedPipeline,
			sip_infra.RegisterActivePipeline,
			sip_infra.RegisterFailedPipeline,
			sip_infra.RegisterExpiringPipeline:
			d.controlCh <- e
		default:
			d.logger.Warnw("OnPipeline: unrouted type", "type", fmt.Sprintf("%T", s))
			d.controlCh <- e
		}
	}
}

func (d *Dispatcher) runDispatcher(ctx context.Context, ch chan callEnvelope) {
	for {
		select {
		case <-ctx.Done():
			d.drain(ch)
			return
		case e := <-ch:
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
	case sip_infra.SessionEstablishedPipeline:
		d.handleSessionEstablished(ctx, v)
	case sip_infra.ByeReceivedPipeline:
		d.handleByeReceived(ctx, v)
	case sip_infra.CancelReceivedPipeline:
		d.handleCancelReceived(ctx, v)
	case sip_infra.TransferInitiatedPipeline:
		d.handleTransferInitiated(ctx, v)
	case sip_infra.TransferConnectedPipeline:
		d.handleTransferConnected(ctx, v)
	case sip_infra.TransferFailedPipeline:
		d.handleTransferFailed(ctx, v)
	case sip_infra.CallEndedPipeline:
		d.handleCallEnded(ctx, v)
	case sip_infra.CallFailedPipeline:
		d.handleCallFailed(ctx, v)
	case sip_infra.EventEmittedPipeline:
		d.handleEventEmitted(ctx, v)
	case sip_infra.MetricEmittedPipeline:
		d.handleMetricEmitted(ctx, v)
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
