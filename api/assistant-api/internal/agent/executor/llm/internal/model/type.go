// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_model

import (
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/protos"
)

// Pipeline remains the router contract consumed by Pipeline(...).
type PipelineType interface{}

type PrepareHistoryProcessPipeline struct {
	Packet internal_type.NormalizedUserTextPacket
}

// ArgumentationProcessPipeline carries per-request state across argumentation stages.
type ArgumentationProcessPipeline struct {
	Packet       internal_type.NormalizedUserTextPacket
	UserMessage  *protos.Message
	History      []*protos.Message
	PromptArgs   map[string]interface{}
	ToolFollowUp bool
}

type AssistantArgumentationProcessPipeline struct {
	Packet       internal_type.NormalizedUserTextPacket
	UserMessage  *protos.Message
	History      []*protos.Message
	PromptArgs   map[string]interface{}
	ToolFollowUp bool
}

type ConversationArgumentationProcessPipeline struct {
	Packet       internal_type.NormalizedUserTextPacket
	UserMessage  *protos.Message
	History      []*protos.Message
	PromptArgs   map[string]interface{}
	ToolFollowUp bool
}

type MessageArgumentationProcessPipeline struct {
	Packet       internal_type.NormalizedUserTextPacket
	UserMessage  *protos.Message
	History      []*protos.Message
	PromptArgs   map[string]interface{}
	ToolFollowUp bool
}

type SessionArgumentationProcessPipeline struct {
	Packet       internal_type.NormalizedUserTextPacket
	UserMessage  *protos.Message
	History      []*protos.Message
	PromptArgs   map[string]interface{}
	ToolFollowUp bool
}

// Output stages
type LLMRequestOutputPipeline struct {
	Packet      internal_type.NormalizedUserTextPacket
	UserMessage *protos.Message
	History     []*protos.Message
	PromptArgs  map[string]interface{}
}

type ToolFollowUpOutputPipeline struct {
	ContextID  string
	PromptArgs map[string]interface{}
}

// LocalHistoryOutputPipeline appends a message to local in-memory history.
type LocalHistoryOutputPipeline struct {
	Message *protos.Message
}

// LLMResponsePipeline is the typed response state flowing through stages.
type LLMResponsePipeline struct {
	Response *protos.ChatResponse

	Output  *protos.Message
	Metrics []*protos.Metric
}

// Backward-compatible aliases used by existing tests and call sites.
type PrepareHistoryPipeline = PrepareHistoryProcessPipeline
type ArgumentationPipeline = ArgumentationProcessPipeline
type AssistantArgumentationPipeline = AssistantArgumentationProcessPipeline
type ConversationArgumentationPipeline = ConversationArgumentationProcessPipeline
type MessageArgumentationPipeline = MessageArgumentationProcessPipeline
type SessionArgumentationPipeline = SessionArgumentationProcessPipeline
type LLMRequestPipeline = LLMRequestOutputPipeline
type ToolFollowUpExecutePipeline = ToolFollowUpOutputPipeline
type LocalHistoryPipeline = LocalHistoryOutputPipeline
