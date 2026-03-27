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
	"io"
	"sync"
	"testing"
	"time"

	internal_agent_executor "github.com/rapidaai/api/assistant-api/internal/agent/executor"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_conversation_entity "github.com/rapidaai/api/assistant-api/internal/entity/conversations"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	integration_client_builders "github.com/rapidaai/pkg/clients/integration/builders"
	"github.com/rapidaai/pkg/commons"
	gorm_models "github.com/rapidaai/pkg/models/gorm"
	gorm_types "github.com/rapidaai/pkg/models/gorm/types"
	rapida_types "github.com/rapidaai/pkg/types"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type packetCollector struct {
	mu   sync.Mutex
	pkts []internal_type.Packet
}

func (c *packetCollector) collect(_ context.Context, pkts ...internal_type.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pkts = append(c.pkts, pkts...)
	return nil
}

func (c *packetCollector) all() []internal_type.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]internal_type.Packet, len(c.pkts))
	copy(out, c.pkts)
	return out
}

// =============================================================================
// Mock: mockStream — grpc.BidiStreamingClient[protos.ChatRequest, protos.ChatResponse]
// =============================================================================

type streamRecvResult struct {
	resp *protos.ChatResponse
	err  error
}

type mockStream struct {
	mu        sync.Mutex
	sendCalls []*protos.ChatRequest
	sendErr   error
	recvCh    chan streamRecvResult
	closeSent bool
}

func newMockStream() *mockStream {
	return &mockStream{
		recvCh: make(chan streamRecvResult, 16),
	}
}

func (m *mockStream) Send(req *protos.ChatRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls = append(m.sendCalls, req)
	return m.sendErr
}

func (m *mockStream) Recv() (*protos.ChatResponse, error) {
	r, ok := <-m.recvCh
	if !ok {
		return nil, io.EOF
	}
	return r.resp, r.err
}

func (m *mockStream) CloseSend() error {
	m.mu.Lock()
	m.closeSent = true
	m.mu.Unlock()
	return nil
}

func (m *mockStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockStream) Trailer() metadata.MD         { return nil }
func (m *mockStream) Context() context.Context     { return context.Background() }
func (m *mockStream) SendMsg(any) error            { return nil }
func (m *mockStream) RecvMsg(any) error            { return nil }

// =============================================================================
// Mock: mockCommunication
// =============================================================================

type mockCommunication struct {
	internal_type.Communication // embedded nil for unimplemented methods
	collector                   *packetCollector
	assistant                   *internal_assistant_entity.Assistant
	args                        map[string]interface{}
	metadata                    map[string]interface{}
	conversation                *internal_conversation_entity.AssistantConversation
	histories                   []internal_type.MessagePacket
	source                      utils.RapidaSource
	mode                        type_enums.MessageMode
}

type noModeCommunication struct {
	internal_type.Communication // embedded nil for unimplemented methods
	assistant                   *internal_assistant_entity.Assistant
	args                        map[string]interface{}
	metadata                    map[string]interface{}
	conversation                *internal_conversation_entity.AssistantConversation
	histories                   []internal_type.MessagePacket
	source                      utils.RapidaSource
}

type unknownPipeline struct {
	stop bool
}

func (p *unknownPipeline) IsStop() bool {
	return p.stop
}

func (m *mockCommunication) OnPacket(ctx context.Context, pkts ...internal_type.Packet) error {
	return m.collector.collect(ctx, pkts...)
}

func (m *mockCommunication) Assistant() *internal_assistant_entity.Assistant {
	return m.assistant
}

func (m *mockCommunication) GetArgs() map[string]interface{} {
	return m.args
}

func (m *mockCommunication) GetOptions() utils.Option {
	return nil
}

func (m *mockCommunication) Source() utils.RapidaSource {
	if m.source == "" {
		return utils.WebPlugin
	}
	return m.source
}

func (m *mockCommunication) Conversation() *internal_conversation_entity.AssistantConversation {
	return m.conversation
}

func (m *mockCommunication) GetHistories() []internal_type.MessagePacket {
	return m.histories
}

func (m *mockCommunication) GetMetadata() map[string]interface{} {
	return m.metadata
}

func (m *mockCommunication) GetMode() type_enums.MessageMode {
	if m.mode == "" {
		return type_enums.TextMode
	}
	return m.mode
}

func (m *noModeCommunication) Assistant() *internal_assistant_entity.Assistant {
	return m.assistant
}

func (m *noModeCommunication) GetArgs() map[string]interface{} {
	return m.args
}

func (m *noModeCommunication) GetOptions() utils.Option {
	return nil
}

func (m *noModeCommunication) Source() utils.RapidaSource {
	if m.source == "" {
		return utils.WebPlugin
	}
	return m.source
}

func (m *noModeCommunication) Conversation() *internal_conversation_entity.AssistantConversation {
	return m.conversation
}

func (m *noModeCommunication) GetHistories() []internal_type.MessagePacket {
	return m.histories
}

func (m *noModeCommunication) GetMetadata() map[string]interface{} {
	return m.metadata
}

// =============================================================================
// Mock: mockToolExecutor
// =============================================================================

type mockToolExecutor struct {
	executeFn func(ctx context.Context, contextID string, calls []*protos.ToolCall, comm internal_type.Communication) *protos.Message
}

var _ internal_agent_executor.ToolExecutor = (*mockToolExecutor)(nil)

func (m *mockToolExecutor) Initialize(context.Context, internal_type.Communication) error {
	return nil
}

func (m *mockToolExecutor) GetFunctionDefinitions() []*protos.FunctionDefinition {
	return nil
}

func (m *mockToolExecutor) ExecuteAll(ctx context.Context, contextID string, calls []*protos.ToolCall, comm internal_type.Communication) *protos.Message {
	if m.executeFn != nil {
		return m.executeFn(ctx, contextID, calls, comm)
	}
	return &protos.Message{Role: "tool"}
}

func (m *mockToolExecutor) Close(context.Context) error {
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

func newTestComm() (*mockCommunication, *packetCollector) {
	c := &packetCollector{}
	return &mockCommunication{
		collector: c,
		assistant: &internal_assistant_entity.Assistant{
			AssistantProviderModel: &internal_assistant_entity.AssistantProviderModel{
				Template:              gorm_types.PromptMap{},
				AssistantModelOptions: []*internal_assistant_entity.AssistantProviderModelOption{},
			},
		},
		source: utils.WebPlugin,
		mode:   type_enums.TextMode,
	}, c
}

func newTestExecutor() *modelAssistantExecutor {
	logger, _ := commons.NewApplicationLogger()
	return &modelAssistantExecutor{
		logger:       logger,
		toolExecutor: &mockToolExecutor{},
		inputBuilder: integration_client_builders.NewChatInputBuilder(logger),
		history:      make([]*protos.Message, 0),
	}
}

func mustLanguage(t *testing.T, iso6391 string) *rapida_types.Language {
	t.Helper()
	lang := rapida_types.LookupLanguage(iso6391)
	require.NotNil(t, lang, "language %q must exist", iso6391)
	return lang
}

func historySnapshot(e *modelAssistantExecutor) []*protos.Message {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*protos.Message, len(e.history))
	copy(out, e.history)
	return out
}

func findPacket[T internal_type.Packet](pkts []internal_type.Packet) (T, bool) {
	for _, p := range pkts {
		if v, ok := p.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

func findPackets[T internal_type.Packet](pkts []internal_type.Packet) []T {
	var out []T
	for _, p := range pkts {
		if v, ok := p.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func TestBuildAssistantArgumentationContext_IncludesNamespaces(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()
	comm.assistant.Id = 123
	comm.assistant.Name = "Lex"
	comm.assistant.Language = "en"
	comm.assistant.Description = "Assistant description"
	comm.args = map[string]interface{}{"custom_key": "custom_value"}

	ctxMap := e.buildAssistantArgumentationContext(comm)
	system, ok := ctxMap["system"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, system["current_date"])
	assert.NotEmpty(t, system["current_time"])
	assert.NotEmpty(t, system["current_datetime"])
	assert.NotEmpty(t, system["day_of_week"])
	assert.NotEmpty(t, system["date_rfc1123"])
	assert.NotEmpty(t, system["date_unix"])
	assert.NotEmpty(t, system["date_unix_ms"])

	assistant, ok := ctxMap["assistant"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Lex", assistant["name"])
	assert.Equal(t, "123", assistant["id"])
	assert.Equal(t, "en", assistant["language"])
	assert.Equal(t, "Assistant description", assistant["description"])

	message, ok := ctxMap["message"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "English", message["language"])

	assert.Equal(t, "custom_value", ctxMap["custom_key"])
	_, hasArgsNamespace := ctxMap["args"]
	assert.True(t, hasArgsNamespace, "arguments should be available via {{args.*}}")
}

func TestBuildConversationArgumentationContext_IncludesConversationFields(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()
	comm.conversation = &internal_conversation_entity.AssistantConversation{
		Audited: gorm_models.Audited{
			Id:          999,
			CreatedDate: gorm_models.TimeWrapper(time.Now().Add(-3 * time.Minute)),
		},
		Identifier: "user-42",
		Source:     utils.PhoneCall,
		Direction:  type_enums.DIRECTION_INBOUND,
	}

	ctxMap := e.buildConversationArgumentationContext(comm)
	conversation, ok := ctxMap["conversation"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "999", conversation["id"])
	assert.Equal(t, "user-42", conversation["identifier"])
	assert.Equal(t, "phone-call", conversation["source"])
	assert.Equal(t, "inbound", conversation["direction"])
	assert.NotEmpty(t, conversation["created_date"])
	assert.NotEmpty(t, conversation["duration"])
}

func TestBuildSessionArgumentationContext_ModeOmittedWhenNotAvailable(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()
	noMode := &noModeCommunication{
		assistant:    comm.assistant,
		args:         comm.args,
		metadata:     comm.metadata,
		conversation: comm.conversation,
		histories:    comm.histories,
		source:       comm.source,
	}

	ctxMap := e.buildSessionArgumentationContext(noMode)

	session, ok := ctxMap["session"].(map[string]interface{})
	require.True(t, ok)
	_, hasMode := session["mode"]
	assert.False(t, hasMode)
}

func TestBuildSessionArgumentationContext_IncludesMode(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()
	comm.mode = type_enums.AudioMode

	ctxMap := e.buildSessionArgumentationContext(comm)

	session, ok := ctxMap["session"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "audio", session["mode"])
}

func TestClonePromptArguments_DeepClone(t *testing.T) {
	e := newTestExecutor()
	in := map[string]interface{}{
		"message": map[string]interface{}{
			"text": "hello",
		},
		"name": "a",
	}
	cloned := e.clonePromptArguments(in)
	nested := cloned["message"].(map[string]interface{})
	nested["text"] = "changed"
	assert.Equal(t, "hello", in["message"].(map[string]interface{})["text"])
}

func TestRequestPipeline_ExecutesAllStages(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, collector := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-pipeline",
		Text:      "test",
		Language:  mustLanguage(t, "en"),
	})
	require.NoError(t, err)

	stream.mu.Lock()
	defer stream.mu.Unlock()
	assert.Len(t, stream.sendCalls, 1, "send_chat stage should have sent")

	evs := findPackets[internal_type.ConversationEventPacket](collector.all())
	require.Len(t, evs, 1, "emit_event stage should have emitted")
	assert.Equal(t, "executing", evs[0].Data["type"])
}

func TestResponsePipeline_ExecutesAllStages(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()
	resp := &protos.ChatResponse{
		RequestId: "req-stages",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{Contents: []string{"hi"}},
			},
		},
		Metrics: []*protos.Metric{{Name: "tokens", Value: "1"}},
	}
	e.executeResponse(context.Background(), comm, resp)

	pkts := collector.all()
	require.GreaterOrEqual(t, len(pkts), 3, "validate + build + emit stages should produce packets")
}

func TestPipeline_LocalHistoryPipeline_AppendsHistory(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()

	err := e.Pipeline(context.Background(), comm, LocalHistoryPipeline{
		Message: &protos.Message{Role: "assistant"},
	})
	require.NoError(t, err)

	snapshot := historySnapshot(e)
	require.Len(t, snapshot, 1)
	assert.Equal(t, "assistant", snapshot[0].Role)
}

func TestPipeline_LocalHistoryPipeline_NilMessageNoOp(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()

	err := e.Pipeline(context.Background(), comm, LocalHistoryPipeline{})
	require.NoError(t, err)
	assert.Empty(t, historySnapshot(e))
}

func TestPipeline_RejectsUnsupportedPipelineType(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()

	err := e.Pipeline(context.Background(), comm, &unknownPipeline{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported pipeline type")
}

func TestPipeline_PrepareHistoryPipeline_ChainsToSendAndAppend(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()

	e.mu.Lock()
	e.history = []*protos.Message{
		{Role: "user", Message: &protos.Message_User{User: &protos.UserMessage{Content: "existing"}}},
	}
	e.currentPacket = &internal_type.NormalizedUserTextPacket{ContextID: "ctx-prepare"}
	e.mu.Unlock()

	pipeline := PrepareHistoryPipeline{
		Packet: internal_type.NormalizedUserTextPacket{
			ContextID: "ctx-prepare",
			Text:      "new input",
			Language:  mustLanguage(t, "en"),
		},
	}
	err := e.Pipeline(context.Background(), comm, pipeline)
	require.NoError(t, err)

	stream.mu.Lock()
	require.Len(t, stream.sendCalls, 1)
	stream.mu.Unlock()

	snapshot := historySnapshot(e)
	require.Len(t, snapshot, 2)
	assert.Equal(t, "existing", snapshot[0].GetUser().GetContent())
	assert.Equal(t, "new input", snapshot[1].GetUser().GetContent())
}

func TestPipeline_ArgumentationPipeline_PreparesPromptArgs(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()
	en := mustLanguage(t, "en")
	comm.args = map[string]interface{}{"name": "lex"}
	comm.assistant.AssistantProviderModel.Template = gorm_types.PromptMap{
		"prompt": []map[string]string{
			{"role": "system", "content": "name={{ name }} msg={{ message.text }} lang={{ message.language }}"},
		},
	}

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-arg",
		Text:      "hello",
		Language:  en,
	})
	require.NoError(t, err)

	stream.mu.Lock()
	require.Len(t, stream.sendCalls, 1)
	require.NotNil(t, stream.sendCalls[0].GetConversations()[0].GetSystem())
	assert.Equal(t, "name=lex msg=hello lang="+en.Name, stream.sendCalls[0].GetConversations()[0].GetSystem().GetContent())
	stream.mu.Unlock()
}

func TestPipeline_ArgumentationPipeline_PriorityOverride_AssistantConversationMessageSession(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()
	en := mustLanguage(t, "en")
	comm.mode = type_enums.AudioMode
	comm.args = map[string]interface{}{
		"assistant": map[string]interface{}{
			"name": "from-args-assistant",
		},
		"conversation": map[string]interface{}{
			"id": "from-args-conversation",
		},
		"message": map[string]interface{}{
			"text":     "from-args-message",
			"language": "from-args-language",
		},
		"session": map[string]interface{}{
			"mode": "from-args-session",
		},
	}
	comm.conversation = &internal_conversation_entity.AssistantConversation{
		Audited: gorm_models.Audited{Id: 99},
	}
	comm.assistant.Name = "from-assistant-stage"
	comm.assistant.AssistantProviderModel.Template = gorm_types.PromptMap{
		"prompt": []map[string]string{
			{"role": "system", "content": "a={{ assistant.name }} c={{ conversation.id }} m={{ message.text }} l={{ message.language }} s={{ session.mode }}"},
		},
	}

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-arg-override",
		Text:      "hello",
		Language:  en,
	})
	require.NoError(t, err)

	stream.mu.Lock()
	require.Len(t, stream.sendCalls, 1)
	require.NotNil(t, stream.sendCalls[0].GetConversations()[0].GetSystem())
	assert.Equal(t, "a=from-args-assistant c=99 m=hello l="+en.Name+" s=audio", stream.sendCalls[0].GetConversations()[0].GetSystem().GetContent())
	stream.mu.Unlock()
}

func TestExecute_MessageLanguage_DefaultEnglishFallback(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()
	comm.assistant.AssistantProviderModel.Template = gorm_types.PromptMap{
		"prompt": []map[string]string{
			{"role": "system", "content": "lang={{ message.language }} text={{ message.text }}"},
		},
	}

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-default-language",
		Text:      "hello",
		Language:  nil,
	})
	require.NoError(t, err)

	stream.mu.Lock()
	require.Len(t, stream.sendCalls, 1)
	require.NotNil(t, stream.sendCalls[0].GetConversations()[0].GetSystem())
	assert.Equal(t, "lang=English text=hello", stream.sendCalls[0].GetConversations()[0].GetSystem().GetContent())
	stream.mu.Unlock()
}

func TestExecute_MessageLanguage_UsesUserTextPacketLanguage(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()
	hi := mustLanguage(t, "hi")
	comm.assistant.AssistantProviderModel.Template = gorm_types.PromptMap{
		"prompt": []map[string]string{
			{"role": "system", "content": "lang={{ message.language }} text={{ message.text }}"},
		},
	}

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-explicit-language",
		Text:      "namaste",
		Language:  hi,
	})
	require.NoError(t, err)

	stream.mu.Lock()
	require.Len(t, stream.sendCalls, 1)
	require.NotNil(t, stream.sendCalls[0].GetConversations()[0].GetSystem())
	assert.Equal(t, "lang="+hi.Name+" text=namaste", stream.sendCalls[0].GetConversations()[0].GetSystem().GetContent())
	stream.mu.Unlock()
}

func TestExecute_MessageLanguage_DottedPromptVariable_DoesNotBreakTemplateParsing(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()
	fr := mustLanguage(t, "fr")
	comm.assistant.AssistantProviderModel.Template = gorm_types.PromptMap{
		"prompt": []map[string]string{
			{"role": "system", "content": "lang={{ message.language }} text={{ message.text }}"},
		},
		"promptVariables": []map[string]string{
			{"name": "message.language", "defaultValue": "English"},
		},
	}

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-dotted-variable",
		Text:      "bonjour",
		Language:  fr,
	})
	require.NoError(t, err)

	stream.mu.Lock()
	require.Len(t, stream.sendCalls, 1)
	require.NotNil(t, stream.sendCalls[0].GetConversations()[0].GetSystem())
	assert.Equal(t, "lang="+fr.Name+" text=bonjour", stream.sendCalls[0].GetConversations()[0].GetSystem().GetContent())
	stream.mu.Unlock()
}

// =============================================================================
// Tests: handleResponse — 5 cases
// =============================================================================

func TestHandleResponse_Error(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.ChatResponse{
		RequestId: "req-1",
		Success:   false,
		Error:     &protos.Error{ErrorMessage: "rate limited"},
	}
	e.executeResponse(context.Background(), comm, resp)

	pkts := collector.all()
	require.Len(t, pkts, 2)

	errPkt, ok := pkts[0].(internal_type.LLMErrorPacket)
	require.True(t, ok)
	assert.Equal(t, "req-1", errPkt.ContextID)
	assert.Equal(t, "rate limited", errPkt.Error.Error())

	ev, ok := pkts[1].(internal_type.ConversationEventPacket)
	require.True(t, ok)
	assert.Equal(t, "error", ev.Data["type"])
	assert.Equal(t, "rate limited", ev.Data["error"])
}

func TestHandleResponse_NilOutput(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.ChatResponse{
		RequestId: "req-2",
		Success:   true,
		Data:      nil,
	}
	e.executeResponse(context.Background(), comm, resp)
	assert.Empty(t, collector.all(), "nil output should emit no packets")
}

func TestHandleResponse_StaleResponse_Dropped(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()
	e.currentPacket = &internal_type.NormalizedUserTextPacket{ContextID: "ctx-active"}
	e.mu.Lock()
	e.history = append(e.history, &protos.Message{Role: "user", Message: &protos.Message_User{User: &protos.UserMessage{Content: "q"}}})
	e.mu.Unlock()

	resp := &protos.ChatResponse{
		RequestId: "ctx-stale",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{Contents: []string{"should-drop"}},
			},
		},
		Metrics: []*protos.Metric{{Name: "tokens", Value: "1"}},
	}
	e.executeResponse(context.Background(), comm, resp)
	assert.Empty(t, collector.all(), "stale response should be ignored")
	snap := historySnapshot(e)
	require.Len(t, snap, 1)
}

func TestValidateHistorySequence_SandwichRuleViolation(t *testing.T) {
	e := newTestExecutor()
	err := e.validateHistorySequence([]*protos.Message{
		{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					ToolCalls: []*protos.ToolCall{{Id: "tc1", Type: "function"}},
				},
			},
		},
		{
			Role:    "user",
			Message: &protos.Message_User{User: &protos.UserMessage{Content: "next"}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immediately followed by tool response")
}

func TestValidateHistorySequence_IDMismatch(t *testing.T) {
	e := newTestExecutor()
	err := e.validateHistorySequence([]*protos.Message{
		{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					ToolCalls: []*protos.ToolCall{{Id: "tc1", Type: "function"}},
				},
			},
		},
		{
			Role: "tool",
			Message: &protos.Message_Tool{
				Tool: &protos.ToolMessage{
					Tools: []*protos.ToolMessage_Tool{{Id: "tc2", Name: "fn", Content: "{}"}},
				},
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing tool response")
}

func TestValidateHistorySequence_OrphanTool(t *testing.T) {
	e := newTestExecutor()
	err := e.validateHistorySequence([]*protos.Message{
		{
			Role: "tool",
			Message: &protos.Message_Tool{
				Tool: &protos.ToolMessage{Tools: []*protos.ToolMessage_Tool{{Id: "tc1"}}},
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "orphan tool response")
}

func TestValidateHistorySequence_StrictSequencingViolation(t *testing.T) {
	e := newTestExecutor()
	err := e.validateHistorySequence([]*protos.Message{
		{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					ToolCalls: []*protos.ToolCall{{Id: "tc1", Type: "function"}},
				},
			},
		},
		{
			Role: "tool",
			Message: &protos.Message_Tool{
				Tool: &protos.ToolMessage{
					Tools: []*protos.ToolMessage_Tool{{Id: "tc1", Name: "fn", Content: "{}"}},
				},
			},
		},
		{
			Role:    "user",
			Message: &protos.Message_User{User: &protos.UserMessage{Content: "oops"}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strict sequencing violated")
}

func TestValidateHistorySequence_ValidChain(t *testing.T) {
	e := newTestExecutor()
	err := e.validateHistorySequence([]*protos.Message{
		{
			Role:    "user",
			Message: &protos.Message_User{User: &protos.UserMessage{Content: "q"}},
		},
		{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					ToolCalls: []*protos.ToolCall{{Id: "tc1", Type: "function"}},
				},
			},
		},
		{
			Role: "tool",
			Message: &protos.Message_Tool{
				Tool: &protos.ToolMessage{
					Tools: []*protos.ToolMessage_Tool{{Id: "tc1", Name: "fn", Content: "{}"}},
				},
			},
		},
		{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{Contents: []string{"done"}},
			},
		},
	})
	require.NoError(t, err)
}

func TestExecuteUserTurn_InvalidHistoryIsNotRejectedByPipeline(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()
	e.mu.Lock()
	e.history = []*protos.Message{
		{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					ToolCalls: []*protos.ToolCall{{Id: "tc1", Type: "function"}},
				},
			},
		},
		{
			Role:    "user",
			Message: &protos.Message_User{User: &protos.UserMessage{Content: "bad"}},
		},
	}
	e.mu.Unlock()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-invalid-history",
		Text:      "new",
		Language:  mustLanguage(t, "en"),
	})
	require.NoError(t, err)
	stream.mu.Lock()
	defer stream.mu.Unlock()
	assert.Len(t, stream.sendCalls, 1)
}

func TestHandleResponse_FinalWithoutToolCalls(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.ChatResponse{
		RequestId: "req-3",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					Contents: []string{"Hello", " world"},
				},
			},
		},
		Metrics: []*protos.Metric{{Name: "tokens", Value: "10"}},
	}
	e.executeResponse(context.Background(), comm, resp)

	pkts := collector.all()
	require.Len(t, pkts, 3)

	done, ok := pkts[0].(internal_type.LLMResponseDonePacket)
	require.True(t, ok)
	assert.Equal(t, "req-3", done.ContextID)
	assert.Equal(t, "Hello world", done.Text)

	ev, ok := pkts[1].(internal_type.ConversationEventPacket)
	require.True(t, ok)
	assert.Equal(t, "completed", ev.Data["type"])
	assert.Equal(t, "11", ev.Data["response_char_count"])

	metric, ok := pkts[2].(internal_type.MessageMetricPacket)
	require.True(t, ok)
	assert.Equal(t, "req-3", metric.ContextID)
	require.Len(t, metric.Metrics, 1)
	assert.Equal(t, "tokens", metric.Metrics[0].Name)

	// Verify history was updated
	snapshot := historySnapshot(e)
	require.Len(t, snapshot, 1)
	assert.Equal(t, "assistant", snapshot[0].Role)
}

func TestHandleResponse_FinalWithToolCalls(t *testing.T) {
	e := newTestExecutor()
	// Set stream to nil so chatWithHistory (inside executeToolCalls) fails
	e.stream = nil
	e.currentPacket = &internal_type.NormalizedUserTextPacket{ContextID: "req-4"} // match the response requestId so it's not dropped as stale
	toolMsg := &protos.Message{Role: "tool"}
	e.toolExecutor = &mockToolExecutor{
		executeFn: func(_ context.Context, _ string, _ []*protos.ToolCall, _ internal_type.Communication) *protos.Message {
			return toolMsg
		},
	}

	comm, collector := newTestComm()

	resp := &protos.ChatResponse{
		RequestId: "req-4",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					Contents:  []string{"calling tool"},
					ToolCalls: []*protos.ToolCall{{Id: "tc1", Type: "function"}},
				},
			},
		},
		Metrics: []*protos.Metric{{Name: "tokens", Value: "5"}},
	}
	e.executeResponse(context.Background(), comm, resp)

	pkts := collector.all()
	// Should have: LLMResponseDonePacket, ConversationEventPacket(completed), MessageMetricPacket, LLMErrorPacket(tool call follow-up failed)
	require.GreaterOrEqual(t, len(pkts), 4)

	done, ok := findPacket[internal_type.LLMResponseDonePacket](pkts)
	require.True(t, ok)
	assert.Equal(t, "calling tool", done.Text)

	errPkts := findPackets[internal_type.LLMErrorPacket](pkts)
	require.Len(t, errPkts, 1)
	assert.Contains(t, errPkts[0].Error.Error(), "tool call follow-up failed")

	// Verify history: output + toolExecution were appended atomically
	snapshot := historySnapshot(e)
	require.Len(t, snapshot, 2, "assistant msg + tool result should be in history")
}

func TestHandleResponse_ToolFollowUpRetainsUserMessageContext(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	fr := mustLanguage(t, "fr")
	e.toolExecutor = &mockToolExecutor{
		executeFn: func(_ context.Context, _ string, _ []*protos.ToolCall, _ internal_type.Communication) *protos.Message {
			return &protos.Message{
				Role: "tool",
				Message: &protos.Message_Tool{Tool: &protos.ToolMessage{
					Tools: []*protos.ToolMessage_Tool{{Id: "tc1", Name: "fn", Content: "{}"}},
				}},
			}
		},
	}
	comm, collector := newTestComm()
	comm.metadata = map[string]interface{}{"client.language": "fr"}
	comm.assistant.AssistantProviderModel.Template = gorm_types.PromptMap{
		"prompt": []map[string]string{
			{
				"role":    "system",
				"content": "lang={{ message.language }} text={{ message.text }}",
			},
		},
		"promptVariables": []map[string]string{},
	}

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "req-tool",
		Text:      "bonjour",
		Language:  fr,
	})
	require.NoError(t, err)

	resp := &protos.ChatResponse{
		RequestId: "req-tool",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					Contents:  []string{"tooling"},
					ToolCalls: []*protos.ToolCall{{Id: "tc1", Type: "function"}},
				},
			},
		},
		Metrics: []*protos.Metric{{Name: "tokens", Value: "5"}},
	}
	e.executeResponse(context.Background(), comm, resp)

	stream.mu.Lock()
	defer stream.mu.Unlock()
	require.Len(t, stream.sendCalls, 2, "initial send + tool follow-up send")
	require.NotNil(t, stream.sendCalls[0].GetConversations()[0].GetSystem())
	require.NotNil(t, stream.sendCalls[1].GetConversations()[0].GetSystem())
	assert.Equal(t, "lang="+fr.Name+" text=bonjour", stream.sendCalls[0].GetConversations()[0].GetSystem().GetContent())
	assert.Equal(t, "lang="+fr.Name+" text=bonjour", stream.sendCalls[1].GetConversations()[0].GetSystem().GetContent(), "tool follow-up must preserve original user packet context")

	errPkts := findPackets[internal_type.LLMErrorPacket](collector.all())
	assert.Empty(t, errPkts)
}

func TestHandleResponse_StreamDelta(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.ChatResponse{
		RequestId: "req-5",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					Contents: []string{"partial"},
				},
			},
		},
		// No Metrics → streaming delta
	}
	e.executeResponse(context.Background(), comm, resp)

	pkts := collector.all()
	require.Len(t, pkts, 2)

	delta, ok := pkts[0].(internal_type.LLMResponseDeltaPacket)
	require.True(t, ok)
	assert.Equal(t, "req-5", delta.ContextID)
	assert.Equal(t, "partial", delta.Text)

	ev, ok := pkts[1].(internal_type.ConversationEventPacket)
	require.True(t, ok)
	assert.Equal(t, "llm", ev.Name)
	assert.Equal(t, "chunk", ev.Data["type"])
	assert.Equal(t, "partial", ev.Data["text"])
	assert.Equal(t, "7", ev.Data["response_char_count"])
}

// =============================================================================
// Tests: streamErrorReason — 4 cases
// =============================================================================

func TestStreamErrorReason(t *testing.T) {
	e := newTestExecutor()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"eof", io.EOF, "server closed connection"},
		{"canceled", status.Error(codes.Canceled, "ctx"), "connection canceled"},
		{"unavailable", status.Error(codes.Unavailable, "down"), "server unavailable"},
		{"other", errors.New("broken pipe"), "broken pipe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.streamErrorReason(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// Tests: history mutex — 3 cases
// =============================================================================

func TestSnapshotHistory_ReturnsCopy(t *testing.T) {
	e := newTestExecutor()
	e.mu.Lock()
	e.history = append(e.history, &protos.Message{Role: "user"})
	e.mu.Unlock()

	snapshot := historySnapshot(e)
	require.Len(t, snapshot, 1)

	// Modify snapshot — should not affect original
	snapshot[0] = &protos.Message{Role: "modified"}
	original := historySnapshot(e)
	assert.Equal(t, "user", original[0].Role, "modifying snapshot should not affect original")
}

func TestConcurrency_HistoryAndSnapshot(t *testing.T) {
	e := newTestExecutor()

	var wg sync.WaitGroup
	wg.Add(2)

	comm, _ := newTestComm()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.Execute(context.Background(), comm, internal_type.StaticPacket{
				ContextID: fmt.Sprintf("ctx-%d", i),
				Text:      fmt.Sprintf("msg-%d", i),
			})
		}
	}()

	// Reader: reads snapshots concurrently
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = historySnapshot(e)
		}
	}()

	wg.Wait()
	snapshot := historySnapshot(e)
	assert.Len(t, snapshot, 100, "all messages should be in history")
}

func TestHistoryClearedAfterClose(t *testing.T) {
	e := newTestExecutor()
	e.mu.Lock()
	e.history = append(e.history, &protos.Message{Role: "user"})
	e.mu.Unlock()

	_ = e.Close(context.Background())

	snapshot := historySnapshot(e)
	assert.Empty(t, snapshot, "history should be empty after Close")
}

// =============================================================================
// Tests: Execute — StaticPacket and UserTextPacket paths
// =============================================================================

func TestExecute_StaticPacket_AppendsHistory(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.StaticPacket{
		ContextID: "ctx-1",
		Text:      "hello",
	})
	require.NoError(t, err)
	assert.Empty(t, collector.all(), "StaticPacket should not emit packets")

	snapshot := historySnapshot(e)
	require.Len(t, snapshot, 1)
	assert.Equal(t, "assistant", snapshot[0].Role)
	assert.Equal(t, []string{"hello"}, snapshot[0].GetAssistant().GetContents())
}

func TestExecute_UserTextPacket_SendsAndRecordsHistory(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, collector := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "say hello",
	})
	require.NoError(t, err)

	evs := findPackets[internal_type.ConversationEventPacket](collector.all())
	require.Len(t, evs, 1)
	assert.Equal(t, "executing", evs[0].Data["type"])
	assert.Equal(t, "say hello", evs[0].Data["script"])
	assert.Equal(t, "9", evs[0].Data["input_char_count"])
	assert.Equal(t, "0", evs[0].Data["history_count"])

	stream.mu.Lock()
	defer stream.mu.Unlock()
	require.Len(t, stream.sendCalls, 1)

	snapshot := historySnapshot(e)
	require.Len(t, snapshot, 1)
	assert.Equal(t, "user", snapshot[0].Role)
	require.NotNil(t, e.currentPacket)
	currentPacket := *e.currentPacket
	assert.Equal(t, "say hello", currentPacket.Text)
	assert.Nil(t, currentPacket.Language)
}

func TestExecute_InterruptionPacket(t *testing.T) {
	e := newTestExecutor()
	e.currentPacket = &internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-old",
		Text:      "old text",
		Language:  mustLanguage(t, "fr"),
	}
	comm, _ := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.InterruptionPacket{ContextID: "x"})
	require.NoError(t, err)
	assert.Nil(t, e.currentPacket, "currentPacket should be nil on interrupt")
}

func TestExecute_UnsupportedPacket(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.EndOfSpeechPacket{ContextID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported packet type")
}

// =============================================================================
// Tests: send — nil stream and success
// =============================================================================

func TestSend_NilStream(t *testing.T) {
	e := newTestExecutor()
	e.stream = nil
	comm, _ := newTestComm()

	err := e.chat(context.Background(), comm, internal_type.NormalizedUserTextPacket{ContextID: "ctx-1"}, map[string]interface{}{}, &protos.Message{Role: "user"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream not connected")
}

func TestSend_Success(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()

	err := e.chat(context.Background(), comm, internal_type.NormalizedUserTextPacket{ContextID: "ctx-1"}, map[string]interface{}{}, &protos.Message{Role: "user"})
	require.NoError(t, err)

	stream.mu.Lock()
	defer stream.mu.Unlock()
	require.Len(t, stream.sendCalls, 1)
	assert.Empty(t, historySnapshot(e), "chat should not append history directly; pipeline stage owns history mutation")
}

// =============================================================================
// Tests: Close — 3 cases
// =============================================================================

func TestClose_ClearsHistoryAndStream(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	e.mu.Lock()
	e.history = append(e.history, &protos.Message{Role: "user"})
	e.currentPacket = &internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "hello",
		Language:  mustLanguage(t, "en"),
	}
	e.mu.Unlock()

	err := e.Close(context.Background())
	require.NoError(t, err)

	e.mu.RLock()
	defer e.mu.RUnlock()
	assert.Nil(t, e.stream)
	assert.Empty(t, e.history)
	assert.Nil(t, e.currentPacket)
}

func TestClose_NoPanicNilStream(t *testing.T) {
	e := newTestExecutor()
	e.stream = nil

	err := e.Close(context.Background())
	require.NoError(t, err, "Close on nil stream should not panic")
}

// =============================================================================
// Tests: listen — processes responses then exits on error
// =============================================================================

func TestListen_RecvEOF(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, collector := newTestComm()

	stream.recvCh <- streamRecvResult{err: io.EOF}

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(context.Background(), comm)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not exit on EOF")
	}

	dirs := findPackets[internal_type.DirectivePacket](collector.all())
	require.Len(t, dirs, 1)
	assert.Equal(t, protos.ConversationDirective_END_CONVERSATION, dirs[0].Directive)
	assert.Equal(t, "server closed connection", dirs[0].Arguments["reason"])
}

func TestListen_NilStream(t *testing.T) {
	e := newTestExecutor()
	e.stream = nil
	comm, _ := newTestComm()

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(context.Background(), comm)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not exit with nil stream")
	}
}

func TestListen_ContextCancelled(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(ctx, comm)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not exit on context cancel")
	}
}

func TestListen_ProcessesMultipleMessages(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, collector := newTestComm()

	// Two deltas then EOF
	stream.recvCh <- streamRecvResult{resp: &protos.ChatResponse{
		RequestId: "r1",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{Contents: []string{"chunk1"}},
			},
		},
	}}
	stream.recvCh <- streamRecvResult{resp: &protos.ChatResponse{
		RequestId: "r1",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{Contents: []string{"chunk2"}},
			},
		},
	}}
	stream.recvCh <- streamRecvResult{err: io.EOF}

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(context.Background(), comm)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not exit")
	}

	deltas := findPackets[internal_type.LLMResponseDeltaPacket](collector.all())
	assert.Len(t, deltas, 2)
}

// =============================================================================
// Tests: Name
// =============================================================================

func TestName(t *testing.T) {
	e := newTestExecutor()
	assert.Equal(t, "model", e.Name())
}

// =============================================================================
// Tests: handleResponse — empty contents delta emits nothing
// =============================================================================

func TestHandleResponse_EmptyContents(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.ChatResponse{
		RequestId: "req-6",
		Success:   true,
		Data: &protos.Message{
			Role: "assistant",
			Message: &protos.Message_Assistant{
				Assistant: &protos.AssistantMessage{
					Contents: []string{},
				},
			},
		},
	}
	e.executeResponse(context.Background(), comm, resp)
	assert.Empty(t, collector.all(), "empty contents should emit no delta")
}

// =============================================================================
// Tests: concurrent listen + close (run with -race)
// =============================================================================

func TestConcurrency_ListenAndClose(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, _ := newTestComm()

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.ctxCancel = cancel

	go func() {
		e.listen(ctx, comm)
	}()

	time.Sleep(5 * time.Millisecond)
	err := e.Close(context.Background())
	require.NoError(t, err)
}

// =============================================================================
// Tests: Execute UserTextPacket includes correct history_count
// =============================================================================

func TestExecute_UserTextPacket_HistoryCount(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream

	e.mu.Lock()
	e.history = append(e.history,
		&protos.Message{Role: "user"},
		&protos.Message{Role: "assistant"},
	)
	e.mu.Unlock()

	comm, collector := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-2",
		Text:      "follow up",
	})
	require.NoError(t, err)

	evs := findPackets[internal_type.ConversationEventPacket](collector.all())
	require.Len(t, evs, 1)
	assert.Equal(t, "2", evs[0].Data["history_count"], "should reflect 2 existing messages")
}

// =============================================================================
// Tests: Execute with stream send error
// =============================================================================

func TestExecute_SendError(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	stream.sendErr = fmt.Errorf("send failed")
	e.stream = stream
	comm, _ := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "test",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send chat request")
}

// =============================================================================
// Tests: Bug 1 — history not modified on send error
// =============================================================================

func TestExecute_SendError_HistoryNotModified(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	stream.sendErr = fmt.Errorf("send failed")
	e.stream = stream
	comm, _ := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "test",
	})
	require.Error(t, err)
	assert.Empty(t, historySnapshot(e), "history must not be modified when send fails")
}

// =============================================================================
// Tests: Bug 3 — listener exits cleanly when context is cancelled before EOF
// =============================================================================

func TestListen_ExitsCleanlyOnClose(t *testing.T) {
	e := newTestExecutor()
	stream := newMockStream()
	e.stream = stream
	comm, collector := newTestComm()

	ctx, cancel := context.WithCancel(context.Background())

	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		e.listen(ctx, comm)
	}()

	// Cancel context first (simulating ctxCancel() in Close()), then unblock Recv.
	cancel()
	close(stream.recvCh)

	select {
	case <-listenDone:
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not exit after context cancellation")
	}

	dirs := findPackets[internal_type.DirectivePacket](collector.all())
	assert.Empty(t, dirs, "END_CONVERSATION must not be dispatched when context is cancelled")
}
