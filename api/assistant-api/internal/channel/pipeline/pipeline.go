// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_pipeline

import (
	"bufio"
	"errors"
	"net"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/types"
	"github.com/rapidaai/protos"
)

var (
	ErrCallbackNotConfigured = errors.New("pipeline callback not configured")
)

type Pipeline interface {
	CallID() string
}

type CallReceivedPipeline struct {
	ID          string
	Provider    string
	Auth        types.SimplePrinciple
	AssistantID uint64
	GinContext  *gin.Context
}

func (p CallReceivedPipeline) CallID() string { return p.ID }

type WebhookParsedPipeline struct {
	ID          string
	Provider    string
	Auth        types.SimplePrinciple
	AssistantID uint64
	CallInfo    *internal_type.CallInfo
	GinContext  *gin.Context
}

func (p WebhookParsedPipeline) CallID() string { return p.ID }

type AssistantResolvedPipeline struct {
	ID          string
	Provider    string
	Auth        types.SimplePrinciple
	AssistantID uint64
	Assistant   *internal_assistant_entity.Assistant
	CallInfo    *internal_type.CallInfo
	GinContext  *gin.Context
}

func (p AssistantResolvedPipeline) CallID() string { return p.ID }

type ConversationCreatedPipeline struct {
	ID             string
	Provider       string
	Auth           types.SimplePrinciple
	AssistantID    uint64
	Assistant      *internal_assistant_entity.Assistant
	ConversationID uint64
	ContextID      string
	CallInfo       *internal_type.CallInfo
	GinContext     *gin.Context
}

func (p ConversationCreatedPipeline) CallID() string { return p.ID }

type ProviderAnsweringPipeline struct {
	ID             string
	Provider       string
	Auth           types.SimplePrinciple
	AssistantID    uint64
	ConversationID uint64
	ContextID      string
	CallerNumber   string
	GinContext     *gin.Context
}

func (p ProviderAnsweringPipeline) CallID() string { return p.ID }

type ProviderAnsweredPipeline struct {
	ID        string
	ContextID string
}

func (p ProviderAnsweredPipeline) CallID() string { return p.ID }

type SessionConnectedPipeline struct {
	ID        string
	ContextID string
	WebSocket *websocket.Conn
	Conn      net.Conn
	Reader    *bufio.Reader
	Writer    *bufio.Writer
}

func (p SessionConnectedPipeline) CallID() string { return p.ID }

type SessionInitializedPipeline struct {
	ID   string
	Auth types.SimplePrinciple
}

func (p SessionInitializedPipeline) CallID() string { return p.ID }

type CallActivePipeline struct {
	ID string
}

func (p CallActivePipeline) CallID() string { return p.ID }

type ModeSwitchPipeline struct {
	ID   string
	From string
	To   string
}

func (p ModeSwitchPipeline) CallID() string { return p.ID }

type DisconnectRequestedPipeline struct {
	ID     string
	Reason string
}

func (p DisconnectRequestedPipeline) CallID() string { return p.ID }

type CallCompletedPipeline struct {
	ID       string
	Duration time.Duration
	Messages int
	Reason   string
}

func (p CallCompletedPipeline) CallID() string { return p.ID }

type CallFailedPipeline struct {
	ID    string
	Stage string
	Error error
}

func (p CallFailedPipeline) CallID() string { return p.ID }

type OutboundRequestedPipeline struct {
	ID          string
	Auth        types.SimplePrinciple
	AssistantID uint64
	Version     string
	ToPhone     string
	FromPhone   string
	Metadata    map[string]interface{}
	Args        map[string]interface{}
	Options     map[string]interface{}
}

func (p OutboundRequestedPipeline) CallID() string { return p.ID }

type OutboundDialedPipeline struct {
	ID       string
	CallInfo *internal_type.CallInfo
}

func (p OutboundDialedPipeline) CallID() string { return p.ID }

type EventEmittedPipeline struct {
	ID    string
	Event string
	Data  map[string]string
}

func (p EventEmittedPipeline) CallID() string { return p.ID }

type MetricEmittedPipeline struct {
	ID      string
	Metrics []*protos.Metric
}

func (p MetricEmittedPipeline) CallID() string { return p.ID }
