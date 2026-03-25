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
	"sync"
	"time"

	internal_agent_executor "github.com/rapidaai/api/assistant-api/internal/agent/executor"
	internal_agent_tool "github.com/rapidaai/api/assistant-api/internal/agent/executor/tool"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	integration_client_builders "github.com/rapidaai/pkg/clients/integration/builders"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ internal_agent_executor.AssistantExecutor = (*modelAssistantExecutor)(nil)

type modelAssistantExecutor struct {
	logger             commons.Logger
	toolExecutor       internal_agent_executor.ToolExecutor
	providerCredential *protos.VaultCredential
	inputBuilder       integration_client_builders.InputChatBuilder
	history            []*protos.Message
	stream             grpc.BidiStreamingClient[protos.ChatRequest, protos.ChatResponse]
	mu                 sync.RWMutex
	activeContextID    string // set by chat(), cleared on interrupt, checked by listener
	ctx                context.Context
	ctxCancel          context.CancelFunc
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
	return g.Wait()
}

func (e *modelAssistantExecutor) chat(
	_ context.Context,
	communication internal_type.Communication,
	contextID string,
	promptArgs map[string]interface{},
	in *protos.Message,
	histories ...*protos.Message,
) error {
	request := e.buildChatRequest(communication, contextID, promptArgs, append(histories, in)...)

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
	e.mu.Lock()
	e.activeContextID = contextID
	e.history = append(e.history, in)
	e.mu.Unlock()
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
	if err := e.validateHistorySequence(snapshot); err != nil {
		return err
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
		e.handleResponse(ctx, communication, resp)
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

// handleResponse processes a single response from the server.
func (e *modelAssistantExecutor) handleResponse(ctx context.Context, communication internal_type.Communication, resp *protos.ChatResponse) {
	e.executeResponseFlow(ctx, communication, resp)
}

// executeToolCalls executes all requested tool calls and sends the follow-up
// chat with both the assistant message and tool results appended atomically.
// The assistant message is NOT yet in e.history — we add both together to
// prevent a concurrent user message from seeing tool_calls without results
// (which causes OpenAI 400 errors).
func (e *modelAssistantExecutor) executeToolCalls(ctx context.Context, communication internal_type.Communication, contextID string, output *protos.Message,
	promptArgs map[string]interface{},
) error {
	toolExecution := e.toolExecutor.ExecuteAll(ctx, contextID, output.GetAssistant().GetToolCalls(), communication)
	e.mu.Lock()
	if e.activeContextID != contextID {
		// Context was interrupted during tool execution — discard results.
		e.mu.Unlock()
		return nil
	}
	e.history = append(e.history, output, toolExecution)
	e.mu.Unlock()
	return e.chatWithHistory(ctx, communication, contextID, promptArgs)
}

// Execute forwards an incoming packet to the LLM.
//
// Emits ConversationEventPacket: {type: "executing"} for UserTextPacket.
func (e *modelAssistantExecutor) Execute(ctx context.Context, communication internal_type.Communication, pctk internal_type.Packet) error {
	return e.executeRequest(ctx, communication, pctk)
}

// snapshotHistory returns a point-in-time copy of the conversation history.
func (e *modelAssistantExecutor) snapshotHistory() []*protos.Message {
	e.mu.RLock()
	snapshot := make([]*protos.Message, len(e.history))
	copy(snapshot, e.history)
	e.mu.RUnlock()
	return snapshot
}

// Close cancels the listener context, tears down the stream, and clears history.
func (e *modelAssistantExecutor) Close(ctx context.Context) error {
	if e.ctxCancel != nil {
		e.ctxCancel()
	}
	e.mu.Lock()
	if e.stream != nil {
		e.stream.CloseSend()
		e.stream = nil
	}
	e.history = make([]*protos.Message, 0)
	e.mu.Unlock()
	return nil
}
