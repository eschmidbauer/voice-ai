// Rapida – Open Source Voice AI Orchestration Platform
// Copyright (C) 2023-2025 Prashant Srivastav <prashant@rapida.ai>
// Licensed under a modified GPL-2.0. See the LICENSE file for details.
package internal_openai_compatible_callers

import (
	"testing"

	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

func newTestLogger() commons.Logger {
	lgr, _ := commons.NewApplicationLogger()
	return lgr
}

func newTestCaller() *largeLanguageCaller {
	return &largeLanguageCaller{
		OpenAICompatible: OpenAICompatible{
			logger:     newTestLogger(),
			credential: func() map[string]interface{} { return map[string]interface{}{} },
		},
	}
}

func TestBuildHistory_MixedMessages(t *testing.T) {
	caller := newTestCaller()
	msgs := []*protos.Message{
		{Role: "system", Message: &protos.Message_System{System: &protos.SystemMessage{Content: "Be brief"}}},
		{Role: "user", Message: &protos.Message_User{User: &protos.UserMessage{Content: "Hi"}}},
		{Role: "assistant", Message: &protos.Message_Assistant{Assistant: &protos.AssistantMessage{Contents: []string{"Hello"}}}},
		{Role: "tool", Message: &protos.Message_Tool{Tool: &protos.ToolMessage{Tools: []*protos.ToolMessage_Tool{{Id: "call_1", Name: "get_weather", Content: `{"temp":72}`}}}}},
	}

	history := caller.buildHistory(msgs)
	require.Len(t, history, 4)
	assert.NotNil(t, history[0].OfSystem)
	assert.NotNil(t, history[1].OfUser)
	assert.NotNil(t, history[2].OfAssistant)
	assert.NotNil(t, history[3].OfTool)
}

func TestBuildHistory_AssistantWithToolCall(t *testing.T) {
	caller := newTestCaller()
	msgs := []*protos.Message{
		{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					ToolCalls: []*protos.ToolCall{
						{
							Id:   "call_1",
							Type: "function",
							Function: &protos.FunctionCall{
								Name:      "get_weather",
								Arguments: `{"city":"NYC"}`,
							},
						},
					},
				},
			},
		},
	}

	history := caller.buildHistory(msgs)
	require.Len(t, history, 1)
	require.NotNil(t, history[0].OfAssistant)
	require.Len(t, history[0].OfAssistant.ToolCalls, 1)
	assert.Equal(t, "call_1", history[0].OfAssistant.ToolCalls[0].ID)
	assert.Equal(t, "get_weather", history[0].OfAssistant.ToolCalls[0].Function.Name)
}

func newCredentialCaller(t *testing.T, credentials map[string]interface{}) *largeLanguageCaller {
	t.Helper()
	value, err := structpb.NewStruct(credentials)
	require.NoError(t, err)
	credential := &protos.Credential{Value: value}
	return NewLargeLanguageCaller(newTestLogger(), credential).(*largeLanguageCaller)
}

func TestGetClient_RequiresBaseURL(t *testing.T) {
	caller := newCredentialCaller(t, map[string]interface{}{"key": "sk-test"})
	_, err := caller.GetClient()
	require.Error(t, err)
}

func TestGetClient_RejectsEmptyBaseURL(t *testing.T) {
	caller := newCredentialCaller(t, map[string]interface{}{"url": "  ", "key": "sk-test"})
	_, err := caller.GetClient()
	require.Error(t, err)
}

func TestGetClient_RejectsNonStringBaseURL(t *testing.T) {
	caller := newCredentialCaller(t, map[string]interface{}{"url": 123, "key": "sk-test"})
	_, err := caller.GetClient()
	require.Error(t, err)
}

func TestGetClient_AllowsMissingAPIKey(t *testing.T) {
	caller := newCredentialCaller(t, map[string]interface{}{"url": "http://localhost:8000/v1"})
	client, err := caller.GetClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestGetClient_UsesConfiguredBaseURL(t *testing.T) {
	caller := newCredentialCaller(t, map[string]interface{}{"url": "http://localhost:8000/v1", "key": "sk-test"})
	client, err := caller.GetClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}
