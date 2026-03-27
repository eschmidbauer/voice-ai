// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

// Package internal_agentkit implements the AgentKit assistant executor.
//
// AgentKit is a gRPC-based protocol that allows external agents to participate
// in Rapida voice conversations. Unlike the model executor (which manages LLM
// calls, history, and tool execution internally), the agentkit executor acts as
// a transparent bridge: the external agent owns the conversation loop, tool
// orchestration, and state management.
//
// # Lifecycle
//
//  1. Initialize — dials the external agent's gRPC endpoint, starts a
//     bidirectional Talk stream, sends ConversationInitialization as the first
//     message, and spawns a listener goroutine.
//  2. Execute (called per user turn) — forwards UserTextPacket to the agent.
//     StaticPacket is a no-op (external agent manages its own history).
//  3. Close — sends CloseSend on the stream, tears down the gRPC connection,
//     and waits for the listener goroutine to exit (with a 5 s timeout).
//
// # ConversationEvent contract
//
// The executor emits ConversationEventPacket at every critical point so the
// debugger, analytics, and webhook pipelines have full visibility. Events use
// Name="agentkit" for agent-level events and Name="tool" for tool observability:
//
//	Initialize  → {type: "agentkit_initialized", provider, url, init_ms}
//	Execute     → {type: "executing",   script, input_char_count}
//	Init ack    → {type: "initialization_ack", conversation_id}
//	Interruption→ {type: "interruption", source}
//	Text chunk  → {type: "chunk",       text, response_char_count}
//	Text done   → {type: "completed",   text, response_char_count}
//	Tool call   → {type: "tool_call",   tool_id, name}          (Name="tool")
//	Tool result → {type: "tool_result",  tool_id, name, success} (Name="tool")
//	Agent error → {type: "error",       error, code}
//	Directive   → (forwarded as-is, no extra event)
package internal_agentkit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	internal_agent_executor "github.com/rapidaai/api/assistant-api/internal/agent/executor"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ internal_agent_executor.AssistantExecutor = (*agentkitExecutor)(nil)

type agentkitExecutor struct {
	logger     commons.Logger
	connection *grpc.ClientConn
	talker     grpc.BidiStreamingClient[protos.TalkInput, protos.TalkOutput]
	mu         sync.RWMutex
	currentID  string
	done       chan struct{} // closed when the listener goroutine exits
}

// NewAgentKitAssistantExecutor creates a new AgentKit-based assistant executor.
func NewAgentKitAssistantExecutor(logger commons.Logger) internal_agent_executor.AssistantExecutor {
	return &agentkitExecutor{
		logger: logger,
	}
}

// Name returns the executor name identifier.
func (e *agentkitExecutor) Name() string {
	return "agentkit"
}

// Initialize establishes the gRPC connection and starts the listener goroutine.
//
// Emits ConversationEventPacket: {type: "agentkit_initialized"}.
func (e *agentkitExecutor) Initialize(ctx context.Context, comm internal_type.Communication, cfg *protos.ConversationInitialization) error {
	start := time.Now()
	provider := comm.Assistant().AssistantProviderAgentkit
	if provider == nil {
		return fmt.Errorf("agentkit provider is not enabled")
	}

	// Connect
	if err := e.connect(ctx, provider); err != nil {
		return err
	}

	// Start listener goroutine — stops on context cancel or server close
	e.done = make(chan struct{})
	utils.Go(ctx, func() {
		defer close(e.done)
		e.listen(ctx, comm)
	})

	// Send initialization as the first message (mirrors the WebTalk flow)
	if err := e.sendInitialization(provider.AssistantId, provider.Id, comm.Conversation().Id, cfg); err != nil {
		return fmt.Errorf("failed to send initialization: %w", err)
	}

	comm.OnPacket(ctx, internal_type.ConversationEventPacket{
		Name: "agentkit",
		Data: map[string]string{
			"type":     "agentkit_initialized",
			"provider": "agentkit",
			"url":      provider.Url,
			"init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
		},
		Time: time.Now(),
	})
	return nil
}

// connect establishes the gRPC connection.
func (e *agentkitExecutor) connect(ctx context.Context, provider *internal_assistant_entity.AssistantProviderAgentkit) error {
	opts := []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt64), grpc.MaxCallSendMsgSize(math.MaxInt64))}
	// credentials and tls
	if provider.Certificate != "" {
		creds, err := e.buildTLSCredentials(provider.Certificate)
		if err != nil {
			return fmt.Errorf("TLS credentials failed: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// create connection with provider url
	conn, err := grpc.NewClient(provider.Url, opts...)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}

	// create client and stream
	client := protos.NewAgentKitClient(conn)
	e.connection = conn

	// Build metadata from provider.Metadata (headers to pass to server)
	streamCtx := ctx
	if len(provider.Metadata) > 0 {
		md := metadata.New(map[string]string(provider.Metadata))
		streamCtx = metadata.NewOutgoingContext(ctx, md)
	}

	talker, err := client.Talk(streamCtx)
	if err != nil {
		return fmt.Errorf("stream start failed: %w", err)
	}
	e.talker = talker
	return nil
}

// buildTLSCredentials creates TLS credentials from a PEM certificate.
// If certPEM is "insecure" or "skip-verify", it skips certificate verification (dev only).
func (e *agentkitExecutor) buildTLSCredentials(certPEM string) (credentials.TransportCredentials, error) {
	// Allow skipping verification for development
	if certPEM == "insecure" || certPEM == "skip-verify" {
		e.logger.Warnf("Using insecure TLS (skipping certificate verification) - DO NOT USE IN PRODUCTION")
		return credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		}), nil
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(certPEM)) {
		e.logger.Errorf("Failed to parse certificate PEM (length=%d, starts=%q)", len(certPEM), certPEM[:min(50, len(certPEM))])
		return nil, fmt.Errorf("invalid certificate: failed to parse PEM")
	}
	return credentials.NewTLS(&tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}), nil
}

// send writes a message to the gRPC stream (thread-safe).
func (e *agentkitExecutor) send(req *protos.TalkInput) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.talker == nil {
		return fmt.Errorf("not connected")
	}
	return e.talker.Send(req)
}

// sendInitialization sends ConversationInitialization as the first message on the stream,
// mirroring the WebTalk flow where initialization is always the first message.
func (e *agentkitExecutor) sendInitialization(assistantId uint64, assistantProviderID uint64, ConversationID uint64, cfg *protos.ConversationInitialization) error {
	return e.send(&protos.TalkInput{
		Request: &protos.TalkInput_Initialization{
			Initialization: &protos.ConversationInitialization{
				AssistantConversationId: ConversationID,
				Assistant: &protos.AssistantDefinition{
					AssistantId: assistantId,
					Version:     utils.GetVersionString(assistantProviderID),
				},
				Args:         cfg.GetArgs(),
				Metadata:     cfg.GetMetadata(),
				Options:      cfg.GetOptions(),
				StreamMode:   cfg.GetStreamMode(),
				UserIdentity: cfg.GetUserIdentity(),
				Time:         timestamppb.New(time.Now()),
			},
		},
	})
}

// listen reads messages from the stream until context is cancelled or connection closes.
// It guards the talker reference with mu.RLock to avoid racing with Close().
func (e *agentkitExecutor) listen(ctx context.Context, comm internal_type.Communication) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		e.mu.RLock()
		talker := e.talker
		e.mu.RUnlock()

		if talker == nil {
			return
		}

		resp, err := talker.Recv()
		if err != nil {
			reason := e.streamErrorReason(err)
			comm.OnPacket(ctx, internal_type.DirectivePacket{
				Directive: protos.ConversationDirective_END_CONVERSATION,
				Arguments: map[string]interface{}{"reason": reason},
			})
			return
		}
		e.handleResponse(ctx, resp, comm)
	}
}

// streamErrorReason maps a stream error to a human-readable reason string.
func (e *agentkitExecutor) streamErrorReason(err error) string {
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

func (e *agentkitExecutor) setCurrentContextID(id string) {
	e.mu.Lock()
	e.currentID = id
	e.mu.Unlock()
}

func (e *agentkitExecutor) isCurrentContextID(id string) bool {
	clean := strings.TrimSpace(id)
	e.mu.RLock()
	defer e.mu.RUnlock()
	current := strings.TrimSpace(e.currentID)
	// Preserve historical behavior for id-less packets while still gating stale ids.
	if clean == "" || current == "" {
		return true
	}
	return clean == current
}

func (e *agentkitExecutor) sendUserMessage(ctx context.Context, comm internal_type.Communication, contextID string, text string) error {
	if strings.TrimSpace(contextID) == "" {
		return nil
	}
	e.setCurrentContextID(contextID)
	comm.OnPacket(ctx, internal_type.ConversationEventPacket{
		ContextID: contextID,
		Name:      "agentkit",
		Data: map[string]string{
			"type":             "executing",
			"script":           text,
			"input_char_count": fmt.Sprintf("%d", len(text)),
		},
		Time: time.Now(),
	})
	return e.send(&protos.TalkInput{
		Request: &protos.TalkInput_Message{
			Message: &protos.ConversationUserMessage{
				Message: &protos.ConversationUserMessage_Text{
					Text: text,
				},
				Id:        contextID,
				Completed: true,
				Time:      timestamppb.Now(),
			},
		},
	})
}

// handleResponse processes a single response from the external agent.
//
// It emits the appropriate Packet(s) for each message type and pairs them with
// ConversationEventPacket where relevant for observability.
func (e *agentkitExecutor) handleResponse(ctx context.Context, resp *protos.TalkOutput, comm internal_type.Communication) {
	switch data := resp.GetData().(type) {
	case *protos.TalkOutput_Initialization:
		// External agent acknowledged ConversationInitialization.
		// Emits: ConversationEventPacket {type: "initialization_ack"}
		e.logger.Debugf("AgentKit initialization acknowledged, conversationId=%d", data.Initialization.GetAssistantConversationId())
		comm.OnPacket(ctx, internal_type.ConversationEventPacket{
			Name: "agentkit",
			Data: map[string]string{
				"type":            "initialization_ack",
				"conversation_id": fmt.Sprintf("%d", data.Initialization.GetAssistantConversationId()),
			},
			Time: time.Now(),
		})

	case *protos.TalkOutput_Interruption:
		if !e.isCurrentContextID(data.Interruption.GetId()) {
			return
		}
		// Emits: InterruptionPacket + ConversationEventPacket {type: "interruption"}
		comm.OnPacket(ctx,
			internal_type.InterruptionPacket{ContextID: data.Interruption.Id, Source: internal_type.InterruptionSourceWord},
			internal_type.ConversationEventPacket{
				ContextID: data.Interruption.Id,
				Name:      "agentkit",
				Data:      map[string]string{"type": "interruption", "source": "word"},
				Time:      time.Now(),
			},
		)

	case *protos.TalkOutput_Assistant:
		if !e.isCurrentContextID(data.Assistant.GetId()) {
			return
		}
		switch msg := data.Assistant.GetMessage().(type) {
		case *protos.ConversationAssistantMessage_Text:
			if data.Assistant.GetCompleted() {
				// Emits: LLMResponseDonePacket + ConversationEventPacket {type: "completed"}
				comm.OnPacket(ctx,
					internal_type.LLMResponseDonePacket{
						ContextID: data.Assistant.GetId(),
						Text:      msg.Text,
					},
					internal_type.ConversationEventPacket{
						ContextID: data.Assistant.GetId(),
						Name:      "agentkit",
						Data: map[string]string{
							"type":                "completed",
							"text":                msg.Text,
							"response_char_count": fmt.Sprintf("%d", len(msg.Text)),
						},
						Time: time.Now(),
					},
				)
				return
			}
			// Streaming delta — emit "chunk" event matching model.go pattern
			comm.OnPacket(ctx,
				internal_type.LLMResponseDeltaPacket{ContextID: data.Assistant.GetId(), Text: msg.Text},
				internal_type.ConversationEventPacket{
					ContextID: data.Assistant.GetId(),
					Name:      "agentkit",
					Data: map[string]string{
						"type":                "chunk",
						"text":                msg.Text,
						"response_char_count": fmt.Sprintf("%d", len(msg.Text)),
					},
					Time: time.Now(),
				},
			)
		case *protos.ConversationAssistantMessage_Audio:
			e.logger.Debugf("Received audio message (not implemented)")
		}

	case *protos.TalkOutput_Tool:
		if !e.isCurrentContextID(data.Tool.GetId()) {
			return
		}
		// External agent notifying Rapida of an in-progress tool call.
		// Emits: ConversationEventPacket {type: "tool_call"} (Name="tool")
		e.logger.Debugf("AgentKit tool call: id=%s toolId=%s name=%s", data.Tool.GetId(), data.Tool.GetToolId(), data.Tool.GetName())
		comm.OnPacket(ctx, internal_type.ConversationEventPacket{
			ContextID: data.Tool.GetId(),
			Name:      "tool",
			Data: map[string]string{
				"type":    "tool_call",
				"tool_id": data.Tool.GetToolId(),
				"name":    data.Tool.GetName(),
			},
			Time: time.Now(),
		})

	case *protos.TalkOutput_ToolResult:
		if !e.isCurrentContextID(data.ToolResult.GetId()) {
			return
		}
		// External agent notifying Rapida of a completed tool result.
		// Emits: ConversationEventPacket {type: "tool_result"} (Name="tool")
		e.logger.Debugf("AgentKit tool result: id=%s toolId=%s name=%s success=%v", data.ToolResult.GetId(), data.ToolResult.GetToolId(), data.ToolResult.GetName(), data.ToolResult.GetSuccess())
		comm.OnPacket(ctx, internal_type.ConversationEventPacket{
			ContextID: data.ToolResult.GetId(),
			Name:      "tool",
			Data: map[string]string{
				"type":    "tool_result",
				"tool_id": data.ToolResult.GetToolId(),
				"name":    data.ToolResult.GetName(),
				"success": fmt.Sprintf("%v", data.ToolResult.GetSuccess()),
			},
			Time: time.Now(),
		})

	case *protos.TalkOutput_Error:
		// External agent sent an error — emit error packets then end conversation.
		// Emits: LLMErrorPacket + ConversationEventPacket {type: "error"} + DirectivePacket
		errMsg := data.Error.GetErrorMessage()
		e.logger.Errorf("AgentKit agent error: code=%d message=%s", data.Error.GetErrorCode(), errMsg)
		comm.OnPacket(ctx,
			internal_type.LLMErrorPacket{
				Error: fmt.Errorf("agentkit error %d: %s", data.Error.GetErrorCode(), errMsg),
			},
			internal_type.ConversationEventPacket{
				Name: "agentkit",
				Data: map[string]string{
					"type":  "error",
					"error": errMsg,
					"code":  fmt.Sprintf("%d", data.Error.GetErrorCode()),
				},
				Time: time.Now(),
			},
			internal_type.DirectivePacket{
				Directive: protos.ConversationDirective_END_CONVERSATION,
				Arguments: map[string]interface{}{"reason": errMsg},
			},
		)

	case *protos.TalkOutput_Directive:
		if !e.isCurrentContextID(data.Directive.GetId()) {
			return
		}
		args, _ := utils.AnyMapToInterfaceMap(data.Directive.GetArgs())
		comm.OnPacket(ctx, internal_type.DirectivePacket{ContextID: data.Directive.GetId(), Directive: data.Directive.GetType(), Arguments: args})
	}
}

// Execute sends a packet to the AgentKit server.
//
// Emits ConversationEventPacket: {type: "executing"} for UserTextPacket.
func (e *agentkitExecutor) Execute(ctx context.Context, comm internal_type.Communication, packet internal_type.Packet) error {
	switch p := packet.(type) {
	case internal_type.NormalizedUserTextPacket:
		return e.sendUserMessage(ctx, comm, p.ContextID, p.Text)
	case internal_type.UserTextPacket:
		return e.sendUserMessage(ctx, comm, p.ContextID, p.Text)
	case internal_type.StaticPacket:
		// No-op: external agent manages its own history
		return nil
	case internal_type.InterruptionPacket:
		e.setCurrentContextID("")
		return nil

	default:
		return fmt.Errorf("unsupported packet: %T", packet)
	}
}

// Close terminates the gRPC connection and waits for the listener goroutine to
// exit (up to 5 s). Safe to call concurrently with listen().
func (e *agentkitExecutor) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.talker != nil {
		e.talker.CloseSend()
		e.talker = nil
	}
	if e.connection != nil {
		e.connection.Close()
		e.connection = nil
	}
	done := e.done
	e.currentID = ""
	e.mu.Unlock()

	// Wait for listener goroutine to exit
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			e.logger.Errorf("Timed out waiting for listener goroutine to exit")
		}
	}
	return nil
}
