// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_model

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	type_enums "github.com/rapidaai/pkg/types/enums"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

// executeRequestFlow routes request packets by type, similar to dispatcher-style
// handlers in adapters. UserTextPacket is processed via request pipeline stages.
func (e *modelAssistantExecutor) executeRequest(ctx context.Context, communication internal_type.Communication, pkt internal_type.Packet) error {
	switch p := pkt.(type) {
	case internal_type.UserTextPacket:
		return e.handleRequestUserText(ctx, communication, p)
	case internal_type.StaticPacket:
		return e.handleRequestStatic(p)
	case internal_type.InterruptionPacket:
		return e.handleRequestInterruption()
	default:
		return fmt.Errorf("unsupported packet type: %T", pkt)
	}
}

func (e *modelAssistantExecutor) handleRequestUserText(ctx context.Context, communication internal_type.Communication, pkt internal_type.UserTextPacket) error {
	return e.executeUserTurn(ctx, communication, pkt)
}

func (e *modelAssistantExecutor) handleRequestStatic(pkt internal_type.StaticPacket) error {
	e.mu.Lock()
	e.history = append(e.history, &protos.Message{
		Role: "assistant",
		Message: &protos.Message_Assistant{Assistant: &protos.AssistantMessage{
			Contents: []string{pkt.Text},
		}},
	})
	e.mu.Unlock()
	return nil
}

func (e *modelAssistantExecutor) handleRequestInterruption() error {
	e.mu.Lock()
	e.activeContextID = ""
	e.mu.Unlock()
	return nil
}

// requestPipeline returns ordered request handlers.
// Add new turn preprocessing steps here.
func (e *modelAssistantExecutor) requestPipeline() []RequestStage {
	return []RequestStage{
		requestStageFunc{name: "build_user_message", fn: e.stageBuildUserMessage},
		requestStageFunc{name: "snapshot_history", fn: e.stageSnapshotHistory},
		requestStageFunc{name: "validate_history", fn: e.stageValidateHistory},
		requestStageFunc{name: "prepare_prompt_arguments", fn: e.stagePreparePromptArguments},
	}
}

func (e *modelAssistantExecutor) initTurn(pkt internal_type.UserTextPacket) LLMRequestPipeline {
	input := InputPipeline{
		ContextID: pkt.ContextID,
		Packet:    pkt,
		UserInput: pkt,
	}
	return LLMRequestPipeline{
		Eval: EvalPipeline{
			History: HistoryPipeline{
				Argumented: ArgumentedPipeline{
					Input: input,
				},
			},
		},
		ContextID: input.ContextID,
	}
}

// executeUserTurn applies the layered flow:
// execute -> preprocess -> argumentation/build context -> chat request.
func (e *modelAssistantExecutor) executeUserTurn(ctx context.Context, communication internal_type.Communication, pkt internal_type.UserTextPacket) error {
	pipeline := e.initTurn(pkt)
	for _, stage := range e.requestPipeline() {
		if pipeline.Eval.Stop {
			return nil
		}
		if err := stage.Run(ctx, communication, &pipeline); err != nil {
			e.logger.Errorf("request stage %s failed: %v", stage.Name(), err)
			return err
		}
	}

	communication.OnPacket(ctx, internal_type.ConversationEventPacket{
		ContextID: pipeline.ContextID,
		Name:      "llm",
		Data: map[string]string{
			"type":             "executing",
			"script":           pkt.Text,
			"input_char_count": fmt.Sprintf("%d", len(pkt.Text)),
			"history_count":    fmt.Sprintf("%d", len(pipeline.History)),
		},
		Time: time.Now(),
	})
	if err := e.chat(ctx, communication, pipeline.ContextID, pipeline.PromptArgs, pipeline.UserMessage, pipeline.History...); err != nil {
		return err
	}
	return nil
}

func (e *modelAssistantExecutor) stageBuildUserMessage(_ context.Context, _ internal_type.Communication, pipeline *LLMRequestPipeline) error {
	pkt, ok := pipeline.Eval.History.Argumented.Input.Packet.(internal_type.UserTextPacket)
	if !ok {
		return fmt.Errorf("unexpected turn packet type: %T", pipeline.Eval.History.Argumented.Input.Packet)
	}
	pipeline.UserMessage = &protos.Message{
		Role: "user",
		Message: &protos.Message_User{
			User: &protos.UserMessage{Content: pkt.Text},
		},
	}
	pipeline.Eval.History.Argumented.UserMessage = pipeline.UserMessage
	return nil
}

func (e *modelAssistantExecutor) stageSnapshotHistory(_ context.Context, _ internal_type.Communication, pipeline *LLMRequestPipeline) error {
	pipeline.History = e.snapshotHistory()
	pipeline.Eval.History.History = pipeline.History
	return nil
}

func (e *modelAssistantExecutor) stageValidateHistory(_ context.Context, _ internal_type.Communication, pipeline *LLMRequestPipeline) error {
	return e.validateHistorySequence(pipeline.History)
}

func (e *modelAssistantExecutor) stagePreparePromptArguments(_ context.Context, communication internal_type.Communication, pipeline *LLMRequestPipeline) error {
	pipeline.PromptArgs = e.preparePromptArguments(communication, pipeline.Eval.History.Argumented.Input.Packet)
	pipeline.Eval.History.Argumented.PromptArgs = pipeline.PromptArgs
	return nil
}

func (e *modelAssistantExecutor) preparePromptArguments(communication internal_type.Communication, packet internal_type.Packet) map[string]interface{} {
	return clonePromptArguments(e.buildPromptContext(communication, packet))
}

func (e *modelAssistantExecutor) preparePromptArgumentsForResponse(communication internal_type.Communication, contextID string) map[string]interface{} {
	e.mu.RLock()
	snapshot := make([]*protos.Message, len(e.history))
	copy(snapshot, e.history)
	e.mu.RUnlock()

	language := e.latestUserLanguage(communication)

	var packet internal_type.Packet
	for i := len(snapshot) - 1; i >= 0; i-- {
		if user := snapshot[i].GetUser(); user != nil {
			packet = internal_type.UserTextPacket{
				ContextID: contextID,
				Text:      user.GetContent(),
				Language:  language,
			}
			break
		}
	}
	return clonePromptArguments(e.buildPromptContext(communication, packet))
}

func (e *modelAssistantExecutor) latestUserLanguage(communication internal_type.Communication) string {
	histories := communication.GetHistories()
	for i := len(histories) - 1; i >= 0; i-- {
		switch h := histories[i].(type) {
		case internal_type.SaveMessagePacket:
			if h.MessageRole == "user" && h.Language != "" {
				return h.Language
			}
		case internal_type.UserTextPacket:
			if h.Language != "" {
				return h.Language
			}
		case *internal_type.UserTextPacket:
			if h != nil && h.Language != "" {
				return h.Language
			}
		}
	}

	if meta := communication.GetMetadata(); meta != nil {
		if s, ok := meta["client.language"].(string); ok {
			return s
		}
	}
	return ""
}

func clonePromptArguments(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if nested, ok := v.(map[string]interface{}); ok {
			out[k] = clonePromptArguments(nested)
			continue
		}
		out[k] = v
	}
	return out
}

// buildPromptContext assembles the full template variable map from Communication
// state and the current turn's message data.
func (e *modelAssistantExecutor) buildPromptContext(communication internal_type.Communication, packet internal_type.Packet) map[string]interface{} {
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

	session := map[string]interface{}{}
	if m, ok := any(communication).(interface{ GetMode() type_enums.MessageMode }); ok {
		session["mode"] = m.GetMode().String()
	}

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

	message := e.messageContext(packet)
	args := communication.GetArgs()

	return utils.MergeMaps(
		map[string]interface{}{"system": system},
		map[string]interface{}{"assistant": assistant},
		map[string]interface{}{"conversation": conversation},
		map[string]interface{}{"session": session},
		map[string]interface{}{"message": message},
		map[string]interface{}{"args": args},
		args,
	)
}

// messageContext maps the current packet into prompt variables under message.*.
func (e *modelAssistantExecutor) messageContext(packet internal_type.Packet) map[string]interface{} {
	message := map[string]interface{}{
		"language": "",
		"text":     "",
	}
	switch p := packet.(type) {
	case internal_type.UserTextPacket:
		message["language"] = p.Language
		message["text"] = p.Text
	case *internal_type.UserTextPacket:
		if p != nil {
			message["language"] = p.Language
			message["text"] = p.Text
		}
	case internal_type.ExecuteLLMPacket:
		message["language"] = p.Language
		message["text"] = p.Input
	case *internal_type.ExecuteLLMPacket:
		if p != nil {
			message["language"] = p.Language
			message["text"] = p.Input
		}
	case internal_type.MessagePacket:
		message["text"] = p.Content()
	}
	return message
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
	systemMessages := e.inputBuilder.Message(
		template.Prompt,
		utils.MergeMaps(e.inputBuilder.PromptArguments(template.Variables), promptArguments),
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
