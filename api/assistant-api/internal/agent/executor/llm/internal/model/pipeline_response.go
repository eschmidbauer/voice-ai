// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/protos"
)

func (e *modelAssistantExecutor) executeResponseFlow(ctx context.Context, communication internal_type.Communication, resp *protos.ChatResponse) {
	if e.isStaleResponse(resp.GetRequestId()) {
		return
	}
	pipeline := &LLMResponsePipeline{
		Response:   resp,
		ContextID:  resp.GetRequestId(),
		PromptArgs: e.preparePromptArgumentsForResponse(communication, resp.GetRequestId()),
	}
	for _, stage := range e.responsePipeline() {
		if pipeline.Stop {
			return
		}
		if err := stage.Run(ctx, communication, pipeline); err != nil {
			e.logger.Errorf("response stage %s failed: %v", stage.Name(), err)
			communication.OnPacket(ctx, internal_type.LLMErrorPacket{
				ContextID: pipeline.ContextID,
				Error:     fmt.Errorf("response stage failed: %w", err),
			})
			return
		}
	}
}

func (e *modelAssistantExecutor) responsePipeline() []ResponseStage {
	return []ResponseStage{
		responseStageFunc{name: "validate_response", fn: e.stageValidateResponse},
		responseStageFunc{name: "build_response_view", fn: e.stageBuildResponseView},
		responseStageFunc{name: "emit_response_upstream", fn: e.stageEmitResponseUpstream},
		responseStageFunc{name: "tool_follow_up", fn: e.stageToolFollowUpResponse},
	}
}

func (e *modelAssistantExecutor) stageValidateResponse(ctx context.Context, communication internal_type.Communication, pipeline *LLMResponsePipeline) error {
	pipeline.Output = pipeline.Response.GetData()
	pipeline.Metrics = pipeline.Response.GetMetrics()

	if !pipeline.Response.GetSuccess() && pipeline.Response.GetError() != nil {
		errMsg := pipeline.Response.GetError().GetErrorMessage()
		communication.OnPacket(ctx,
			internal_type.LLMErrorPacket{
				ContextID: pipeline.ContextID,
				Error:     errors.New(errMsg),
			},
			internal_type.ConversationEventPacket{
				ContextID: pipeline.ContextID,
				Name:      "llm",
				Data:      map[string]string{"type": "error", "error": errMsg},
				Time:      time.Now(),
			},
		)
		pipeline.Stop = true
		return nil
	}
	if pipeline.Output == nil {
		pipeline.Stop = true
		return nil
	}
	return nil
}

func (e *modelAssistantExecutor) stageBuildResponseView(_ context.Context, _ internal_type.Communication, pipeline *LLMResponsePipeline) error {
	if len(pipeline.Metrics) == 0 {
		return nil
	}
	pipeline.HasToolCalls = len(pipeline.Output.GetAssistant().GetToolCalls()) > 0
	pipeline.ResponseText = strings.Join(pipeline.Output.GetAssistant().GetContents(), "")
	pipeline.Now = time.Now()
	return nil
}

func (e *modelAssistantExecutor) stageEmitResponseUpstream(ctx context.Context, communication internal_type.Communication, pipeline *LLMResponsePipeline) error {
	if len(pipeline.Metrics) > 0 {
		if !pipeline.HasToolCalls {
			e.mu.Lock()
			e.history = append(e.history, pipeline.Output)
			e.mu.Unlock()
		}
		communication.OnPacket(ctx,
			internal_type.LLMResponseDonePacket{
				ContextID: pipeline.ContextID,
				Text:      pipeline.ResponseText,
			},
			internal_type.ConversationEventPacket{
				ContextID: pipeline.ContextID,
				Name:      "llm",
				Data: map[string]string{
					"type":                "completed",
					"text":                pipeline.ResponseText,
					"response_char_count": fmt.Sprintf("%d", len(pipeline.ResponseText)),
					"finish_reason":       pipeline.Response.GetFinishReason(),
				},
				Time: pipeline.Now,
			},
			internal_type.MessageMetricPacket{
				ContextID: pipeline.ContextID,
				Metrics:   pipeline.Metrics,
			},
		)
		return nil
	}
	if len(pipeline.Output.GetAssistant().GetContents()) > 0 {
		text := strings.Join(pipeline.Output.GetAssistant().GetContents(), "")
		communication.OnPacket(ctx,
			internal_type.LLMResponseDeltaPacket{
				ContextID: pipeline.ContextID,
				Text:      text,
			},
			internal_type.ConversationEventPacket{
				Name: "llm",
				Data: map[string]string{
					"type":                "chunk",
					"text":                text,
					"response_char_count": fmt.Sprintf("%d", len(text)),
				},
				Time: time.Now(),
			},
		)
	}
	return nil
}

func (e *modelAssistantExecutor) stageToolFollowUpResponse(ctx context.Context, communication internal_type.Communication, pipeline *LLMResponsePipeline) error {
	if len(pipeline.Metrics) == 0 || !pipeline.HasToolCalls {
		return nil
	}
	if err := e.executeToolCalls(ctx, communication, pipeline.ContextID, pipeline.Output, pipeline.PromptArgs); err != nil {
		communication.OnPacket(ctx, internal_type.LLMErrorPacket{
			ContextID: pipeline.ContextID,
			Error:     fmt.Errorf("tool call follow-up failed: %w", err),
		})
	}
	return nil
}

func (e *modelAssistantExecutor) isStaleResponse(requestID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.activeContextID != "" && requestID != e.activeContextID
}
