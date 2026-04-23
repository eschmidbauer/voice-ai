// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_tool_local

import (
	"context"
	"testing"

	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferCallCaller_Call_EmitsToolCallWithAction(t *testing.T) {
	collector := &packetCollector{}
	comm := &mockCommunication{collector: collector}

	caller := &transferCallCaller{toolCaller: toolCaller{toolOptions: &internal_assistant_entity.AssistantTool{Name: "transfer_call"}}, transferTo: "+15551234567"}
	args := map[string]interface{}{"extra": "data"}

	caller.Call(context.Background(), "ctx-100", "tool-200", args, comm)

	pkts := collector.all()
	require.Len(t, pkts, 1, "expected single LLMToolCallPacket with Action")

	tc, ok := pkts[0].(internal_type.LLMToolCallPacket)
	require.True(t, ok, "packet should be LLMToolCallPacket, got %T", pkts[0])
	assert.Equal(t, "tool-200", tc.ToolID)
	assert.Equal(t, "ctx-100", tc.ContextID)
	assert.Equal(t, "transfer_call", tc.Name)
	assert.Equal(t, protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION, tc.Action)
	assert.Equal(t, "+15551234567", tc.Arguments["to"])
}

func TestTransferCallCaller_Call_NoToolResultPacket(t *testing.T) {
	collector := &packetCollector{}
	comm := &mockCommunication{collector: collector}

	caller := &transferCallCaller{toolCaller: toolCaller{toolOptions: &internal_assistant_entity.AssistantTool{Name: "transfer_call"}}, transferTo: "sip:agent@example.com"}
	args := map[string]interface{}{}

	caller.Call(context.Background(), "ctx-1", "tool-1", args, comm)

	pkts := collector.all()
	for _, p := range pkts {
		_, isToolResult := p.(internal_type.LLMToolResultPacket)
		assert.False(t, isToolResult, "transfer_call should not emit LLMToolResultPacket")
	}
}

func TestTransferCallCaller_Call_PacketOrder(t *testing.T) {
	collector := &packetCollector{}
	comm := &mockCommunication{collector: collector}

	caller := &transferCallCaller{
		toolCaller: toolCaller{toolOptions: &internal_assistant_entity.AssistantTool{Name: "transfer_call"}},
		transferTo: "+15559876543",
	}
	args := map[string]interface{}{"reason": "escalation"}

	caller.Call(context.Background(), "ctx-ord-2", "tool-ord-2", args, comm)

	pkts := collector.all()
	require.Len(t, pkts, 1, "expected single LLMToolCallPacket")

	tc, ok := pkts[0].(internal_type.LLMToolCallPacket)
	require.True(t, ok, "packet[0] must be LLMToolCallPacket, got %T", pkts[0])
	assert.Equal(t, "tool-ord-2", tc.ToolID)
	assert.Equal(t, "transfer_call", tc.Name)
	assert.Equal(t, "ctx-ord-2", tc.ContextID)
	assert.Equal(t, protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION, tc.Action)
	assert.Equal(t, "+15559876543", tc.Arguments["to"])
}

func TestTransferCallCaller_Call_SIPTarget(t *testing.T) {
	collector := &packetCollector{}
	comm := &mockCommunication{collector: collector}

	caller := &transferCallCaller{
		toolCaller: toolCaller{toolOptions: &internal_assistant_entity.AssistantTool{Name: "transfer_call"}},
		transferTo: "sip:support@pbx.example.com",
	}
	args := map[string]interface{}{}

	caller.Call(context.Background(), "ctx-sip", "tool-sip", args, comm)

	pkts := collector.all()
	require.Len(t, pkts, 1)

	tc, ok := pkts[0].(internal_type.LLMToolCallPacket)
	require.True(t, ok, "packet[0] must be LLMToolCallPacket")
	assert.Equal(t, "tool-sip", tc.ToolID)
	assert.Equal(t, protos.ToolCallAction_TOOL_CALL_ACTION_TRANSFER_CONVERSATION, tc.Action)
	assert.Equal(t, "sip:support@pbx.example.com", tc.Arguments["to"])
}

func TestTransferCallCaller_Call_TransferToFromArgsWithSeparator(t *testing.T) {
	collector := &packetCollector{}
	comm := &mockCommunication{collector: collector}

	caller := &transferCallCaller{
		toolCaller: toolCaller{toolOptions: &internal_assistant_entity.AssistantTool{Name: "transfer_call"}},
		transferTo: "+15550000000",
	}
	args := map[string]interface{}{"transfer_to": "+15551111111SEPERATOR+15552222222"}

	caller.Call(context.Background(), "ctx-sep", "tool-sep", args, comm)

	pkts := collector.all()
	require.Len(t, pkts, 1)
	tc, ok := pkts[0].(internal_type.LLMToolCallPacket)
	require.True(t, ok, "packet[0] must be LLMToolCallPacket")
	assert.Equal(t, "+15551111111SEPERATOR+15552222222", tc.Arguments["to"], "caller passes raw targets — streamer splits")
}
