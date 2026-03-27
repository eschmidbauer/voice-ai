// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

// Package internal_model implements the model-based assistant executor.
//
// The model executor manages the full LLM conversation loop internally: it
// maintains conversation history, builds chat requests with system prompts,
// streams responses via a persistent bidirectional gRPC connection to the
// integration-api, and orchestrates tool calls when the LLM requests them.
//
// # Lifecycle
//
//  1. Initialize — fetches provider credentials and initializes tools in
//     parallel, opens a persistent StreamChat bidi stream, and spawns a
//     listener goroutine.
//  2. Execute (called per user turn) — snapshots history, builds a chat
//     request, sends it, and appends the user message to history on success.
//  3. Close — cancels the listener context, tears down the stream, and
//     clears history. The listener goroutine exits asynchronously.
//
// # ConversationEvent contract
//
// The executor emits ConversationEventPacket at every critical point so the
// debugger, analytics, and webhook pipelines have full visibility:
//
//	Initialize      → {type: "llm_initialized", provider, init_ms, ...model options}
//	Execute (user)  → {type: "executing",  script, input_char_count, history_count}
//	Response error  → {type: "error",      error}
//	Response done   → {type: "completed",  text, response_char_count, finish_reason}
//	Tool call error → LLMErrorPacket (no separate event — error is on the follow-up send)
package internal_model

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	internal_agent_executor "github.com/rapidaai/api/assistant-api/internal/agent/executor"
	internal_agent_tool "github.com/rapidaai/api/assistant-api/internal/agent/executor/tool"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	integration_client_builders "github.com/rapidaai/pkg/clients/integration/builders"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/parsers"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ internal_agent_executor.AssistantExecutor = (*modelAssistantExecutor)(nil)

type modelAssistantExecutor struct {
	logger commons.Logger

	// toolExecutor is used to execute tool calls requested by the LLM and append results to history atomically with the assistant message.
	toolExecutor internal_agent_executor.ToolExecutor

	// providerCredential is fetched on Initialize and used for all chat requests. It is not modified after initialization, so it does not require synchronization.
	providerCredential *protos.VaultCredential
	inputBuilder       integration_client_builders.InputChatBuilder

	// building history
	history []*protos.Message

	// stream is set on Initialize and cleared on Close. It is used by both the Execute method (to send chat requests) and the listener goroutine (to receive responses), so access is synchronized via mu.
	stream grpc.BidiStreamingClient[protos.ChatRequest, protos.ChatResponse]

	mu            sync.RWMutex
	currentPacket *internal_type.NormalizedUserTextPacket

	//
	ctx       context.Context
	ctxCancel context.CancelFunc
}

func NewModelAssistantExecutor(logger commons.Logger) internal_agent_executor.AssistantExecutor {
	return &modelAssistantExecutor{
		logger:       logger,
		inputBuilder: integration_client_builders.NewChatInputBuilder(logger),
		toolExecutor: internal_agent_tool.NewToolExecutor(logger),
		history:      make([]*protos.Message, 0),
	}
}

// Name returns the executor name identifier.
func (e *modelAssistantExecutor) Name() string {
	return "model"
}

// Initialize fetches credentials, opens the StreamChat bidi stream, and spawns
// the listener goroutine.
//
// Emits ConversationEventPacket: {type: "llm_initialized"}.
func (e *modelAssistantExecutor) Initialize(ctx context.Context, communication internal_type.Communication, cfg *protos.ConversationInitialization) error {
	start := time.Now()
	g, gCtx := errgroup.WithContext(ctx)
	var providerCredential *protos.VaultCredential

	g.Go(func() error {
		credentialID, err := communication.Assistant().AssistantProviderModel.GetOptions().GetUint64("rapida.credential_id")
		if err != nil {
			e.logger.Errorf("Error while getting provider model credential ID: %v", err)
			return fmt.Errorf("failed to get credential ID: %w", err)
		}
		cred, err := communication.VaultCaller().GetCredential(gCtx, communication.Auth(), credentialID)
		if err != nil {
			e.logger.Errorf("Error while getting provider model credentials: %v", err)
			return fmt.Errorf("failed to get provider credential: %w", err)
		}
		providerCredential = cred
		return nil
	})

	g.Go(func() error {
		if err := e.toolExecutor.Initialize(gCtx, communication); err != nil {
			e.logger.Errorf("Error initializing tool executor: %v", err)
			return fmt.Errorf("failed to initialize tool executor: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		e.logger.Errorf("Error during initialization: %v", err)
		return err
	}

	e.providerCredential = providerCredential
	stream, err := communication.IntegrationCaller().StreamChat(
		ctx,
		communication.Auth(),
		communication.Assistant().AssistantProviderModel.ModelProviderName,
	)
	if err != nil {
		e.logger.Errorf("Failed to open stream: %v", err)
		return fmt.Errorf("failed to open stream: %w", err)
	}
	e.stream = stream

	e.ctx, e.ctxCancel = context.WithCancel(ctx)
	utils.Go(e.ctx, func() {
		e.listen(e.ctx, communication)
	})

	llmData := communication.Assistant().AssistantProviderModel.GetOptions().ToStringMap()
	llmData["type"] = "llm_initialized"
	llmData["provider"] = communication.Assistant().AssistantProviderModel.ModelProviderName
	llmData["init_ms"] = fmt.Sprintf("%d", time.Since(start).Milliseconds())
	communication.OnPacket(ctx, internal_type.ConversationEventPacket{
		Name: "llm",
		Data: llmData,
		Time: time.Now(),
	})
	return nil
}

func (e *modelAssistantExecutor) chat(
	_ context.Context,
	communication internal_type.Communication,
	pkt internal_type.NormalizedUserTextPacket,
	promptArgs map[string]interface{},
	in *protos.Message,
	histories ...*protos.Message,
) error {
	request := e.buildChatRequest(communication, pkt.ContextId(), promptArgs, append(histories, in)...)

	e.mu.RLock()
	stream := e.stream
	e.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("stream not connected")
	}
	if err := stream.Send(request); err != nil {
		e.logger.Errorf("error sending chat request: %v", err)
		return fmt.Errorf("failed to send chat request: %w", err)
	}

	return nil
}

// chatWithHistory sends a chat request using all messages already in e.history.
// Unlike chat(), it does not append any new message — the caller is responsible
// for ensuring history is already up-to-date before calling this.
func (e *modelAssistantExecutor) chatWithHistory(
	_ context.Context,
	communication internal_type.Communication,
	contextID string,
	promptArgs map[string]interface{},
) error {
	e.mu.RLock()
	stream := e.stream
	snapshot := make([]*protos.Message, len(e.history))
	copy(snapshot, e.history)
	e.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("stream not connected")
	}
	request := e.buildChatRequest(communication, contextID, promptArgs, snapshot...)
	if err := stream.Send(request); err != nil {
		e.logger.Errorf("error sending chat request: %v", err)
		return fmt.Errorf("failed to send chat request: %w", err)
	}
	return nil
}

// listen reads messages from the stream until context is cancelled or the
// connection closes.
func (e *modelAssistantExecutor) listen(ctx context.Context, communication internal_type.Communication) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		e.mu.RLock()
		stream := e.stream
		e.mu.RUnlock()

		if stream == nil {
			return
		}

		resp, err := stream.Recv()
		if err != nil {
			// If the context was cancelled (e.g., by Close()), exit without
			// dispatching END_CONVERSATION — the caller initiated the teardown.
			select {
			case <-ctx.Done():
				return
			default:
			}
			reason := e.streamErrorReason(err)
			communication.OnPacket(ctx, internal_type.DirectivePacket{
				Directive: protos.ConversationDirective_END_CONVERSATION,
				Arguments: map[string]interface{}{"reason": reason},
			})
			return
		}
		e.executeResponse(ctx, communication, resp)
	}
}

// streamErrorReason maps a stream error to a human-readable reason string.
func (e *modelAssistantExecutor) streamErrorReason(err error) string {
	e.logger.Debugf("Listener received error: %v", err)
	switch {
	case errors.Is(err, io.EOF):
		return "server closed connection"
	case status.Code(err) == codes.Canceled:
		return "connection canceled"
	case status.Code(err) == codes.Unavailable:
		return "server unavailable"
	default:
		return err.Error()
	}
}

// executeToolCalls executes all requested tool calls and sends the follow-up
// chat with both the assistant message and tool results appended atomically.
// The assistant message is NOT yet in e.history — we add both together to
// prevent a concurrent user message from seeing tool_calls without results
// (which causes OpenAI 400 errors).
func (e *modelAssistantExecutor) executeToolCalls(ctx context.Context, communication internal_type.Communication, contextID string, output *protos.Message,
) error {
	toolExecution := e.toolExecutor.ExecuteAll(ctx, contextID, output.GetAssistant().GetToolCalls(), communication)
	e.mu.Lock()
	if e.currentPacket != nil && e.currentPacket.ContextId() != contextID {
		e.mu.Unlock()
		return nil
	}
	history := make([]*protos.Message, len(e.history))
	copy(history, e.history)
	e.history = append(e.history, output, toolExecution)
	activePacket := e.currentPacket
	e.mu.Unlock()
	if activePacket == nil {
		return e.chatWithHistory(ctx, communication, contextID, map[string]interface{}{})
	}
	return e.Pipeline(ctx, communication, ArgumentationPipeline{
		Packet:       *activePacket,
		History:      history,
		PromptArgs:   map[string]interface{}{},
		ToolFollowUp: true,
	})
}

// Execute forwards an incoming packet to the LLM.
//
// Emits ConversationEventPacket: {type: "executing"} for UserTextPacket.
func (e *modelAssistantExecutor) Execute(ctx context.Context, communication internal_type.Communication, pctk internal_type.Packet) error {
	switch p := pctk.(type) {
	case internal_type.NormalizedUserTextPacket:
		if strings.TrimSpace(p.ContextID) == "" {
			return nil
		}
		e.mu.Lock()
		e.currentPacket = &p
		e.mu.Unlock()
		return e.Pipeline(ctx, communication, PrepareHistoryProcessPipeline{
			Packet: p,
		})
	case internal_type.StaticPacket:
		return e.Pipeline(ctx, communication, LocalHistoryPipeline{
			Message: &protos.Message{
				Role: "assistant",
				Message: &protos.Message_Assistant{Assistant: &protos.AssistantMessage{
					Contents: []string{p.Text},
				}},
			},
		})
	case internal_type.InterruptionPacket:
		e.mu.Lock()
		e.currentPacket = nil
		e.mu.Unlock()
		return nil
	}
	return fmt.Errorf("unsupported packet type: %T", pctk)
}

// Close cancels the listener context, tears down the stream, and clears history.
func (e *modelAssistantExecutor) Close(ctx context.Context) error {
	if e.ctxCancel != nil {
		e.ctxCancel()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stream != nil {
		e.stream.CloseSend()
		e.stream = nil
	}
	e.currentPacket = nil
	e.history = make([]*protos.Message, 0)
	return nil
}

func (e *modelAssistantExecutor) currentContextID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.currentPacket == nil {
		return ""
	}
	return e.currentPacket.ContextID
}

func (e *modelAssistantExecutor) isCurrentContext(contextID string) bool {
	if strings.TrimSpace(contextID) == "" {
		return false
	}
	return e.currentContextID() == contextID
}

// =============================================================================
// Pipeline Request Routing
// =============================================================================
// Pipeline is the central recursive router. Packets enter and get transformed
// into pipeline types; pipeline types get their stages executed.
func (e *modelAssistantExecutor) Pipeline(ctx context.Context, communication internal_type.Communication, v PipelineType) error {
	switch p := v.(type) {
	case LocalHistoryOutputPipeline:
		if p.Message == nil {
			return nil
		}
		e.mu.Lock()
		e.history = append(e.history, p.Message)
		e.mu.Unlock()
		return nil
	case PrepareHistoryProcessPipeline:
		if !e.isCurrentContext(p.Packet.ContextID) {
			return nil
		}
		e.mu.RLock()
		history := make([]*protos.Message, len(e.history))
		copy(history, e.history)
		e.mu.RUnlock()
		return e.Pipeline(ctx, communication, AssistantArgumentationProcessPipeline{
			Packet: p.Packet,
			UserMessage: &protos.Message{
				Role: "user",
				Message: &protos.Message_User{
					User: &protos.UserMessage{Content: p.Packet.Text},
				},
			},
			History:    history,
			PromptArgs: map[string]interface{}{},
		})
	case ArgumentationProcessPipeline:
		if !e.isCurrentContext(p.Packet.ContextID) {
			return nil
		}
		return e.Pipeline(ctx, communication, AssistantArgumentationProcessPipeline{
			Packet:       p.Packet,
			UserMessage:  p.UserMessage,
			History:      p.History,
			PromptArgs:   p.PromptArgs,
			ToolFollowUp: p.ToolFollowUp,
		})
	case AssistantArgumentationProcessPipeline:
		if !e.isCurrentContext(p.Packet.ContextID) {
			return nil
		}
		return e.Pipeline(ctx, communication, ConversationArgumentationProcessPipeline{
			Packet:       p.Packet,
			UserMessage:  p.UserMessage,
			History:      p.History,
			PromptArgs:   utils.MergeMaps(p.PromptArgs, e.buildAssistantArgumentationContext(communication)),
			ToolFollowUp: p.ToolFollowUp,
		})
	case ConversationArgumentationProcessPipeline:
		if !e.isCurrentContext(p.Packet.ContextID) {
			return nil
		}
		return e.Pipeline(ctx, communication, MessageArgumentationProcessPipeline{
			Packet:       p.Packet,
			UserMessage:  p.UserMessage,
			History:      p.History,
			PromptArgs:   utils.MergeMaps(p.PromptArgs, e.buildConversationArgumentationContext(communication)),
			ToolFollowUp: p.ToolFollowUp,
		})
	case MessageArgumentationProcessPipeline:
		if !e.isCurrentContext(p.Packet.ContextID) {
			return nil
		}
		return e.Pipeline(ctx, communication, SessionArgumentationProcessPipeline{
			Packet:       p.Packet,
			UserMessage:  p.UserMessage,
			History:      p.History,
			PromptArgs:   utils.MergeMaps(p.PromptArgs, e.buildMessageArgumentationContext(p.Packet)),
			ToolFollowUp: p.ToolFollowUp,
		})
	case SessionArgumentationProcessPipeline:
		if !e.isCurrentContext(p.Packet.ContextID) {
			return nil
		}
		promptArgs := utils.MergeMaps(p.PromptArgs, e.buildSessionArgumentationContext(communication))
		if p.ToolFollowUp {
			return e.Pipeline(ctx, communication, ToolFollowUpOutputPipeline{
				ContextID:  p.Packet.ContextID,
				PromptArgs: promptArgs,
			})
		}
		return e.Pipeline(ctx, communication, LLMRequestOutputPipeline{
			Packet:      p.Packet,
			UserMessage: p.UserMessage,
			History:     p.History,
			PromptArgs:  promptArgs,
		})
	case LLMRequestOutputPipeline:
		if !e.isCurrentContext(p.Packet.ContextID) {
			return nil
		}
		communication.OnPacket(ctx, internal_type.ConversationEventPacket{
			ContextID: p.Packet.ContextID,
			Name:      "llm",
			Data: map[string]string{
				"type":             "executing",
				"script":           p.Packet.Text,
				"input_char_count": fmt.Sprintf("%d", len(p.Packet.Text)),
				"history_count":    fmt.Sprintf("%d", len(p.History)),
			},
			Time: time.Now(),
		})
		if err := e.chat(ctx, communication, p.Packet, p.PromptArgs, p.UserMessage, p.History...); err != nil {
			return err
		}
		return e.Pipeline(ctx, communication, LocalHistoryOutputPipeline{
			Message: p.UserMessage,
		})
	case ToolFollowUpOutputPipeline:
		if !e.isCurrentContext(p.ContextID) {
			return nil
		}
		return e.chatWithHistory(ctx, communication, p.ContextID, p.PromptArgs)
	case LLMResponsePipeline:
		if p.Response == nil || strings.TrimSpace(p.Response.GetRequestId()) == "" {
			return nil
		}
		pipeline, err := e.stageValidateResponse(ctx, communication, p)
		if err != nil {
			return err
		}
		if pipeline.Response == nil {
			return nil
		}
		if err := e.stageEmitResponseUpstream(ctx, communication, pipeline); err != nil {
			return err
		}
		if err := e.stageToolFollowUpResponse(ctx, communication, pipeline); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported pipeline type: %T", v)
	}
}

func (e *modelAssistantExecutor) clonePromptArguments(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if nested, ok := v.(map[string]interface{}); ok {
			out[k] = e.clonePromptArguments(nested)
			continue
		}
		out[k] = v
	}
	return out
}

func (e *modelAssistantExecutor) buildAssistantArgumentationContext(communication internal_type.Communication) map[string]interface{} {
	now := time.Now().UTC()
	system := map[string]interface{}{
		"current_date":     now.Format("2006-01-02"),
		"current_time":     now.Format("15:04:05"),
		"current_datetime": now.Format(time.RFC3339),
		"day_of_week":      now.Weekday().String(),
		"date_rfc1123":     now.Format(time.RFC1123),
		"date_unix":        strconv.FormatInt(now.Unix(), 10),
		"date_unix_ms":     strconv.FormatInt(now.UnixMilli(), 10),
	}

	assistant := map[string]interface{}{}
	if a := communication.Assistant(); a != nil {
		assistant = map[string]interface{}{
			"name":        a.Name,
			"id":          fmt.Sprintf("%d", a.Id),
			"language":    a.Language,
			"description": a.Description,
		}
	}

	args := communication.GetArgs()
	return utils.MergeMaps(
		map[string]interface{}{"system": system},
		map[string]interface{}{"assistant": assistant},
		map[string]interface{}{"message": map[string]interface{}{"language": "English"}},
		map[string]interface{}{"args": args},
		args,
	)
}

func (e *modelAssistantExecutor) buildConversationArgumentationContext(communication internal_type.Communication) map[string]interface{} {
	conversation := map[string]interface{}{}
	if conv := communication.Conversation(); conv != nil {
		conversation["id"] = fmt.Sprintf("%d", conv.Id)
		conversation["identifier"] = conv.Identifier
		conversation["source"] = string(conv.Source)
		conversation["direction"] = conv.Direction.String()
		if startTime := time.Time(conv.CreatedDate); !startTime.IsZero() {
			conversation["created_date"] = startTime.UTC().Format(time.RFC3339)
			conversation["duration"] = time.Since(startTime).Truncate(time.Second).String()
		}
		if updated := time.Time(conv.UpdatedDate); !updated.IsZero() {
			conversation["updated_date"] = updated.UTC().Format(time.RFC3339)
		}
	}
	return map[string]interface{}{"conversation": conversation}
}

func (e *modelAssistantExecutor) buildMessageArgumentationContext(packet internal_type.NormalizedUserTextPacket) map[string]interface{} {
	message := map[string]interface{}{
		"text": "",
	}
	if packet.Language != nil {
		message["language"] = packet.Language.Name
		message["language_code"] = packet.Language.ISO639_1
	}
	message["text"] = packet.Text
	return map[string]interface{}{"message": message}
}

type modeCommunication interface {
	GetMode() type_enums.MessageMode
}

func (e *modelAssistantExecutor) buildSessionArgumentationContext(communication internal_type.Communication) map[string]interface{} {
	session := map[string]interface{}{}
	if modeComm, ok := communication.(modeCommunication); ok {
		if mode := modeComm.GetMode(); mode != "" {
			session["mode"] = mode.String()
		}
	}
	return map[string]interface{}{"session": session}
}

// validateHistorySequence enforces tool-call sequencing invariants:
//  1. Sandwich rule: assistant(tool_call) must be immediately followed by tool.
//  2. ID matching: tool message IDs must exactly match preceding tool_call IDs.
//  3. No orphans: tool without matching preceding assistant(tool_call) is invalid.
//  4. Strict sequencing: after tool response, next message (if any) must be assistant.
func (e *modelAssistantExecutor) validateHistorySequence(messages []*protos.Message) error {
	for i, msg := range messages {
		assistant := msg.GetAssistant()
		tool := msg.GetTool()

		if assistant != nil && len(assistant.GetToolCalls()) > 0 {
			if i+1 >= len(messages) || messages[i+1].GetTool() == nil {
				return fmt.Errorf("history invalid: assistant tool_call at index %d is not immediately followed by tool response", i)
			}
			if err := e.validateToolIDMatch(assistant.GetToolCalls(), messages[i+1].GetTool().GetTools(), i); err != nil {
				return err
			}
		}

		if tool != nil {
			if i == 0 {
				return fmt.Errorf("history invalid: orphan tool response at index %d without preceding assistant tool_call", i)
			}
			prevAssistant := messages[i-1].GetAssistant()
			if prevAssistant == nil || len(prevAssistant.GetToolCalls()) == 0 {
				return fmt.Errorf("history invalid: orphan tool response at index %d without preceding assistant tool_call", i)
			}
			if err := e.validateToolIDMatch(prevAssistant.GetToolCalls(), tool.GetTools(), i-1); err != nil {
				return err
			}
			if i+1 < len(messages) && messages[i+1].GetAssistant() == nil {
				return fmt.Errorf("history invalid: strict sequencing violated at index %d, expected assistant after tool response", i)
			}
		}
	}
	return nil
}

func (e *modelAssistantExecutor) validateToolIDMatch(calls []*protos.ToolCall, tools []*protos.ToolMessage_Tool, assistantIdx int) error {
	expected := map[string]struct{}{}
	for _, c := range calls {
		if id := strings.TrimSpace(c.GetId()); id != "" {
			expected[id] = struct{}{}
		}
	}
	actual := map[string]struct{}{}
	for _, t := range tools {
		if id := strings.TrimSpace(t.GetId()); id != "" {
			actual[id] = struct{}{}
		}
	}

	for id := range expected {
		if _, ok := actual[id]; !ok {
			return fmt.Errorf("history invalid: missing tool response for tool_call_id %q from assistant index %d", id, assistantIdx)
		}
	}
	for id := range actual {
		if _, ok := expected[id]; !ok {
			return fmt.Errorf("history invalid: orphan tool response id %q at assistant index %d", id, assistantIdx)
		}
	}
	return nil
}

// buildChatRequest constructs the chat request with all necessary parameters.
// The caller provides the complete conversation messages (system prompt is prepended automatically).
func (e *modelAssistantExecutor) buildChatRequest(communication internal_type.Communication, contextID string, promptArguments map[string]interface{}, messages ...*protos.Message) *protos.ChatRequest {
	assistant := communication.Assistant()
	template := assistant.AssistantProviderModel.Template.GetTextChatCompleteTemplate()
	defaultArgs := parsers.CanonicalizePromptArguments(e.inputBuilder.PromptArguments(template.Variables))
	runtimeArgs := parsers.CanonicalizePromptArguments(promptArguments)
	systemMessages := e.inputBuilder.Message(
		template.Prompt,
		utils.MergeMaps(defaultArgs, runtimeArgs),
	)
	req := e.inputBuilder.Chat(
		contextID,
		&protos.Credential{
			Id:    e.providerCredential.GetId(),
			Value: e.providerCredential.GetValue(),
		},
		e.inputBuilder.Options(utils.MergeMaps(assistant.AssistantProviderModel.GetOptions(), communication.GetOptions()), nil),
		e.toolExecutor.GetFunctionDefinitions(),
		map[string]string{
			"assistant_id":                fmt.Sprintf("%d", assistant.Id),
			"message_id":                  contextID,
			"assistant_provider_model_id": fmt.Sprintf("%d", assistant.AssistantProviderModel.Id),
		},
		append(systemMessages, messages...)...,
	)
	req.ProviderName = strings.ToLower(assistant.AssistantProviderModel.ModelProviderName)
	return req
}

// =============================================================================
// Pipeline Response Stages
// =============================================================================
func (e *modelAssistantExecutor) executeResponse(ctx context.Context, communication internal_type.Communication, resp *protos.ChatResponse) {
	if e.isStaleResponse(resp.GetRequestId()) {
		return
	}
	if err := e.Pipeline(ctx, communication, LLMResponsePipeline{
		Response: resp,
	}); err != nil {
		e.logger.Errorf("response pipeline failed: %v", err)
		communication.OnPacket(ctx, internal_type.LLMErrorPacket{
			ContextID: resp.GetRequestId(),
			Error:     fmt.Errorf("response pipeline failed: %w", err),
		})
	}
}

func (e *modelAssistantExecutor) stageValidateResponse(ctx context.Context, communication internal_type.Communication, pipeline LLMResponsePipeline) (LLMResponsePipeline, error) {
	contextID := pipeline.Response.GetRequestId()
	pipeline.Output = pipeline.Response.GetData()
	pipeline.Metrics = pipeline.Response.GetMetrics()

	if !pipeline.Response.GetSuccess() && pipeline.Response.GetError() != nil {
		errMsg := pipeline.Response.GetError().GetErrorMessage()
		communication.OnPacket(ctx,
			internal_type.LLMErrorPacket{
				ContextID: contextID,
				Error:     errors.New(errMsg),
			},
			internal_type.ConversationEventPacket{
				ContextID: contextID,
				Name:      "llm",
				Data:      map[string]string{"type": "error", "error": errMsg},
				Time:      time.Now(),
			},
		)
		pipeline.Response = nil
		return pipeline, nil
	}
	if pipeline.Output == nil {
		pipeline.Response = nil
		return pipeline, nil
	}
	return pipeline, nil
}

func (e *modelAssistantExecutor) stageEmitResponseUpstream(ctx context.Context, communication internal_type.Communication, pipeline LLMResponsePipeline) error {
	contextID := pipeline.Response.GetRequestId()
	if len(pipeline.Metrics) > 0 {
		hasToolCalls := len(pipeline.Output.GetAssistant().GetToolCalls()) > 0
		if !hasToolCalls {
			e.mu.Lock()
			e.history = append(e.history, pipeline.Output)
			e.mu.Unlock()
		}
		responseText := strings.Join(pipeline.Output.GetAssistant().GetContents(), "")
		now := time.Now()
		communication.OnPacket(ctx,
			internal_type.LLMResponseDonePacket{
				ContextID: contextID,
				Text:      responseText,
			},
			internal_type.ConversationEventPacket{
				ContextID: contextID,
				Name:      "llm",
				Data: map[string]string{
					"type":                "completed",
					"text":                responseText,
					"response_char_count": fmt.Sprintf("%d", len(responseText)),
					"finish_reason":       pipeline.Response.GetFinishReason(),
				},
				Time: now,
			},
			internal_type.MessageMetricPacket{
				ContextID: contextID,
				Metrics:   pipeline.Metrics,
			},
		)
		return nil
	}
	if len(pipeline.Output.GetAssistant().GetContents()) > 0 {
		text := strings.Join(pipeline.Output.GetAssistant().GetContents(), "")
		communication.OnPacket(ctx,
			internal_type.LLMResponseDeltaPacket{
				ContextID: contextID,
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

func (e *modelAssistantExecutor) stageToolFollowUpResponse(ctx context.Context, communication internal_type.Communication, pipeline LLMResponsePipeline) error {
	if len(pipeline.Metrics) == 0 {
		return nil
	}
	hasToolCalls := len(pipeline.Output.GetAssistant().GetToolCalls()) > 0
	if !hasToolCalls {
		return nil
	}
	contextID := pipeline.Response.GetRequestId()
	if err := e.executeToolCalls(ctx, communication, contextID, pipeline.Output); err != nil {
		communication.OnPacket(ctx, internal_type.LLMErrorPacket{
			ContextID: contextID,
			Error:     fmt.Errorf("tool call follow-up failed: %w", err),
		})
	}
	return nil
}

func (e *modelAssistantExecutor) isStaleResponse(requestID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentPacket != nil && requestID != e.currentPacket.ContextId()
}
