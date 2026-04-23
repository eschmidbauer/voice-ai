// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_tool_local

import (
	"context"
	"fmt"
	"time"

	internal_tool "github.com/rapidaai/api/assistant-api/internal/agent/executor/tool/internal"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
)

type transferCallCaller struct {
	toolCaller
	transferTo      string
	transferDelay   uint32
	transferMessage string
}

func (tc *transferCallCaller) Call(ctx context.Context, contextID, toolId string, args map[string]interface{}, communication internal_type.Communication) {
	if to, ok := args["transfer_to"].(string); ok && to != "" {
		tc.transferTo = to
	}

	if msg, ok := args["transfer_message"].(string); ok && msg != "" {
		tc.transferMessage = msg
	}

	if delay, ok := args["transfer_delay"].(float64); ok {
		tc.transferDelay = uint32(delay)
	}

	if tc.transferMessage != "" {
		communication.OnPacket(ctx,
			internal_type.InjectMessagePacket{ContextID: contextID, Text: tc.transferMessage},
		)
	}

	if tc.transferDelay > 0 {
		time.Sleep(time.Duration(tc.transferDelay) * time.Millisecond)
	}
	communication.OnPacket(ctx,
		internal_type.LLMToolCallPacket{
			ToolID:    toolId,
			Name:      tc.Name(),
			ContextID: contextID,
			Action:    protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION,
			Arguments: map[string]string{
				"to":      tc.transferTo,
				"message": tc.transferMessage, "delay": fmt.Sprintf("%d", tc.transferDelay)},
		})
}

func NewTransferCallCaller(ctx context.Context, logger commons.Logger, toolOptions *internal_assistant_entity.AssistantTool, communication internal_type.Communication,
) (internal_tool.ToolCaller, error) {
	opts := toolOptions.GetOptions()
	transferTo, err := opts.GetString("tool.transfer_to")
	if err != nil {
		return nil, fmt.Errorf("tool.transfer_to is required: %v", err)
	}
	transferDelay, _ := opts.GetUint32("tool.transfer_delay")
	transferMessage, _ := opts.GetString("tool.transfer_message")
	return &transferCallCaller{
		toolCaller: toolCaller{
			logger:      logger,
			toolOptions: toolOptions,
		},
		transferMessage: transferMessage,
		transferDelay:   transferDelay,
		transferTo:      transferTo,
	}, nil
}
