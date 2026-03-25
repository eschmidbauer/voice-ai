// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_model

import (
	"context"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/protos"
)

// InputPipeline is the first typed artifact entering model orchestration.
type InputPipeline struct {
	ContextID string
	Packet    internal_type.Packet
	UserInput internal_type.UserTextPacket
}

// ArgumentedPipeline holds argumentation output (prompt args + normalized user message).
type ArgumentedPipeline struct {
	Input       InputPipeline
	PromptArgs  map[string]interface{}
	UserMessage *protos.Message
}

// HistoryPipeline enriches argumented input with current conversation history.
type HistoryPipeline struct {
	Argumented ArgumentedPipeline
	History    []*protos.Message
}

// EvalPipeline is a generic hook-point before request construction.
// Add fields here for eval scores, compaction directives, cancellation, etc.
type EvalPipeline struct {
	History HistoryPipeline
	Stop    bool
}

// LLMRequestPipeline is the final typed request artifact before chat send.
type LLMRequestPipeline struct {
	Eval        EvalPipeline
	ContextID   string
	PromptArgs  map[string]interface{}
	UserMessage *protos.Message
	History     []*protos.Message
}

// LLMResponsePipeline is the typed response artifact flowing through response stages.
type LLMResponsePipeline struct {
	Response     *protos.ChatResponse
	ContextID    string
	PromptArgs   map[string]interface{}
	Output       *protos.Message
	Metrics      []*protos.Metric
	HasToolCalls bool
	ResponseText string
	Now          time.Time
	Stop         bool
}

// OutputPipeline is the final hook-point before packets are emitted upstream.
type OutputPipeline struct {
	Response LLMResponsePipeline
	Stop     bool
}

type RequestStage interface {
	Name() string
	Run(ctx context.Context, communication internal_type.Communication, pipeline *LLMRequestPipeline) error
}

type ResponseStage interface {
	Name() string
	Run(ctx context.Context, communication internal_type.Communication, pipeline *LLMResponsePipeline) error
}

type requestStageFunc struct {
	name string
	fn   func(context.Context, internal_type.Communication, *LLMRequestPipeline) error
}

func (s requestStageFunc) Name() string { return s.name }
func (s requestStageFunc) Run(ctx context.Context, communication internal_type.Communication, pipeline *LLMRequestPipeline) error {
	return s.fn(ctx, communication, pipeline)
}

type responseStageFunc struct {
	name string
	fn   func(context.Context, internal_type.Communication, *LLMResponsePipeline) error
}

func (s responseStageFunc) Name() string { return s.name }
func (s responseStageFunc) Run(ctx context.Context, communication internal_type.Communication, pipeline *LLMResponsePipeline) error {
	return s.fn(ctx, communication, pipeline)
}
