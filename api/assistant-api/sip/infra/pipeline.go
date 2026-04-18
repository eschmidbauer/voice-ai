// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package sip_infra

import (
	"time"

	"github.com/rapidaai/pkg/types"
	"github.com/rapidaai/protos"
)

// Pipeline is the base interface for all SIP call lifecycle stages.
// Each concrete type represents a distinct stage in the pipeline.
// Handlers receive a typed Pipeline, apply logic, and emit the next stage(s)
// via OnPipeline — forming chains without explicit wiring.
type Pipeline interface {
	CallID() string
}

// =============================================================================
// Media pipeline — RTP, codec, session establishment
// =============================================================================

// SessionEstablishedPipeline is emitted after RTP allocation, session creation,
// and 200 OK is sent. Converges inbound and outbound flows.
type SessionEstablishedPipeline struct {
	ID              string
	Session         *Session
	Config          *Config
	VaultCredential *protos.VaultCredential
	Direction       CallDirection
	AssistantID     uint64
	Auth            types.SimplePrinciple
	FromURI         string
	ConversationID  uint64 // Non-zero for outbound (already created by channel pipeline)
}

func (p SessionEstablishedPipeline) CallID() string { return p.ID }

// =============================================================================
// Signal pipeline — BYE, CANCEL, transfer (preempts everything)
// =============================================================================

type ByeReceivedPipeline struct {
	ID      string
	Session *Session
	Reason  string
}

func (p ByeReceivedPipeline) CallID() string { return p.ID }

type CancelReceivedPipeline struct {
	ID      string
	Session *Session
}

func (p CancelReceivedPipeline) CallID() string { return p.ID }

type TransferInitiatedPipeline struct {
	ID          string
	Session     *Session
	TargetURI   string
	Config      *Config
	OnConnected func(outboundRTP *RTPHandler)
	OnFailed    func()
}

func (p TransferInitiatedPipeline) CallID() string { return p.ID }

type TransferConnectedPipeline struct {
	ID              string
	InboundSession  *Session
	OutboundSession *Session
}

func (p TransferConnectedPipeline) CallID() string { return p.ID }

type TransferFailedPipeline struct {
	ID     string
	Error  error
	Reason string
}

func (p TransferFailedPipeline) CallID() string { return p.ID }

type CallEndedPipeline struct {
	ID       string
	Duration time.Duration
	Reason   string
}

func (p CallEndedPipeline) CallID() string { return p.ID }

type CallFailedPipeline struct {
	ID      string
	Session *Session
	Error   error
	SIPCode int
}

func (p CallFailedPipeline) CallID() string { return p.ID }

// =============================================================================
// Control pipeline — metrics, events, recording, DTMF, registration
// =============================================================================

// EventEmittedPipeline is a generic event for logging and observability.
type EventEmittedPipeline struct {
	ID    string
	Event string
	Data  map[string]string
}

func (p EventEmittedPipeline) CallID() string { return p.ID }

// MetricEmittedPipeline carries metrics for a call.
type MetricEmittedPipeline struct {
	ID      string
	Metrics []*protos.Metric
}

func (p MetricEmittedPipeline) CallID() string { return p.ID }

// DTMFReceivedPipeline is emitted when a DTMF digit is detected via RTP (RFC 4733).
type DTMFReceivedPipeline struct {
	ID       string
	Digit    string
	Duration int // milliseconds
}

func (p DTMFReceivedPipeline) CallID() string { return p.ID }

// RegisterRequestedPipeline is emitted to initiate SIP REGISTER for a DID.
type RegisterRequestedPipeline struct {
	ID           string // synthetic, e.g. "reg-{DID}"
	DID          string
	Registration *Registration
}

func (p RegisterRequestedPipeline) CallID() string { return p.ID }

// RegisterActivePipeline is emitted when a SIP registration succeeds.
type RegisterActivePipeline struct {
	ID          string
	DID         string
	AssistantID uint64
	ExpiresAt   time.Time
}

func (p RegisterActivePipeline) CallID() string { return p.ID }

// RegisterFailedPipeline is emitted when a SIP registration fails.
type RegisterFailedPipeline struct {
	ID    string
	DID   string
	Error error
}

func (p RegisterFailedPipeline) CallID() string { return p.ID }

// RegisterExpiringPipeline is emitted when a registration is about to expire and needs renewal.
type RegisterExpiringPipeline struct {
	ID  string
	DID string
}

func (p RegisterExpiringPipeline) CallID() string { return p.ID }
