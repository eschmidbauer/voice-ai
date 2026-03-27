// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	internal_agent_executor "github.com/rapidaai/api/assistant-api/internal/agent/executor"
	internal_assistant_entity "github.com/rapidaai/api/assistant-api/internal/entity/assistants"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
	"github.com/rapidaai/protos"
)

var _ internal_agent_executor.AssistantExecutor = (*websocketExecutor)(nil)

type websocketExecutor struct {
	logger    commons.Logger
	conn      *websocket.Conn
	writeMu   sync.Mutex
	contextMu sync.RWMutex
	currentID string
}

// NewWebsocketAssistantExecutor creates a new WebSocket-based assistant executor.
func NewWebsocketAssistantExecutor(logger commons.Logger) internal_agent_executor.AssistantExecutor {
	return &websocketExecutor{
		logger: logger,
	}
}

// Name returns the executor name identifier.
func (e *websocketExecutor) Name() string {
	return "websocket"
}

// Initialize establishes the WebSocket connection and starts the listener.
func (e *websocketExecutor) Initialize(ctx context.Context, comm internal_type.Communication, cfg *protos.ConversationInitialization) error {
	provider := comm.Assistant().AssistantProviderWebsocket
	if provider == nil {
		return fmt.Errorf("websocket provider is not enabled")
	}

	// Connect
	if err := e.connect(ctx, provider); err != nil {
		return err
	}

	// Start listener - stops on context cancel or server close
	utils.Go(ctx, func() {
		if err := e.listen(ctx, comm.OnPacket); err != nil && ctx.Err() == nil {
			comm.OnPacket(ctx, internal_type.DirectivePacket{Directive: protos.ConversationDirective_END_CONVERSATION, Arguments: map[string]interface{}{"reason": err.Error()}})
		}
	})

	// Send initial configuration
	if err := e.sendConfiguration(provider.AssistantId, provider.Id, comm.Conversation().Id, cfg); err != nil {
		return fmt.Errorf("failed to send configuration: %w", err)
	}
	return nil
}

// connect establishes the WebSocket connection.
func (e *websocketExecutor) connect(ctx context.Context, provider *internal_assistant_entity.AssistantProviderWebsocket) error {
	headers := http.Header{}
	for k, v := range provider.Headers {
		headers.Set(k, v)
	}

	wsURL, err := url.Parse(provider.Url)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	query := wsURL.Query()
	for k, v := range provider.Parameters {
		query.Set(k, v)
	}
	wsURL.RawQuery = query.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL.String(), headers)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}

	conn.SetReadLimit(10 * 1024 * 1024)
	e.conn = conn
	return nil
}

// send writes a message to the WebSocket.
func (e *websocketExecutor) send(msg Request) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.conn == nil {
		return fmt.Errorf("not connected")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return e.conn.WriteMessage(websocket.TextMessage, data)
}

// sendConfiguration sends the initial configuration.
func (e *websocketExecutor) sendConfiguration(assistantId uint64, assistantProviderID uint64, conversationID uint64, cfg *protos.ConversationInitialization) error {
	return e.send(Request{
		Type:      TypeConfiguration,
		Timestamp: time.Now().UnixMilli(),
		Data: ConfigurationData{
			AssistantID:    assistantId,
			ConversationID: conversationID,
		},
	})
}

func (e *websocketExecutor) setCurrentContextID(id string) {
	e.contextMu.Lock()
	e.currentID = id
	e.contextMu.Unlock()
}

func (e *websocketExecutor) isCurrentContextID(id string) bool {
	clean := strings.TrimSpace(id)
	e.contextMu.RLock()
	defer e.contextMu.RUnlock()
	current := strings.TrimSpace(e.currentID)
	// Preserve historical behavior for id-less packets while still gating stale ids.
	if clean == "" || current == "" {
		return true
	}
	return clean == current
}

func (e *websocketExecutor) sendUserMessage(contextID string, text string) error {
	if strings.TrimSpace(contextID) == "" {
		return nil
	}
	e.setCurrentContextID(contextID)
	return e.send(Request{
		Type:      TypeUserMessage,
		Timestamp: time.Now().UnixMilli(),
		Data:      UserMessageData{ID: contextID, Content: text},
	})
}

// listen reads messages from WebSocket until context is cancelled or connection closes.
func (e *websocketExecutor) listen(ctx context.Context, onPacket func(ctx context.Context, packet ...internal_type.Packet) error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Allow periodic context checks
		e.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		_, data, err := e.conn.ReadMessage()
		if err != nil {
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				onPacket(ctx, internal_type.DirectivePacket{Directive: protos.ConversationDirective_END_CONVERSATION, Arguments: map[string]interface{}{"reason": "websocket closed the connection"}})
				return nil
			}
			onPacket(ctx, internal_type.DirectivePacket{Directive: protos.ConversationDirective_END_CONVERSATION, Arguments: map[string]interface{}{"reason": err.Error()}})
			return nil
		}

		var resp Response
		if err := json.Unmarshal(data, &resp); err != nil {
			e.logger.Errorf("Invalid response: %v", err)
			continue
		}

		e.handleResponse(ctx, &resp, onPacket)
	}
}

// handleResponse processes a single response from the server.
func (e *websocketExecutor) handleResponse(ctx context.Context, resp *Response, onPacket func(ctx context.Context, packet ...internal_type.Packet) error) {
	switch resp.Type {
	case TypeError:
		var d ErrorData
		json.Unmarshal(resp.Data, &d)
		e.logger.Errorf("Error: %d - %s", d.Code, d.Message)

	case TypeStream:
		var d StreamData
		json.Unmarshal(resp.Data, &d)
		if !e.isCurrentContextID(d.ID) {
			return
		}
		onPacket(ctx, internal_type.LLMResponseDeltaPacket{ContextID: d.ID, Text: d.Content})

	case TypeComplete:
		var d CompleteData
		json.Unmarshal(resp.Data, &d)
		if !e.isCurrentContextID(d.ID) {
			return
		}
		if d.Content != "" {
			onPacket(ctx, internal_type.LLMResponseDonePacket{
				ContextID: d.ID,
				Text:      d.Content,
			})
		}

	// case TypeToolCall:
	// 	var d ToolCallData
	// 	json.Unmarshal(resp.Data, &d)
	// 	onPacket(ctx, internal_type.LLMToolCallPacket{ContextID: d.ID, Name: d.Name, Action: e.mapToolAction(d.Name), Result: d.Params})

	case TypeInterruption:
		var d InterruptionData
		json.Unmarshal(resp.Data, &d)
		if !e.isCurrentContextID(d.ID) {
			return
		}
		source := internal_type.InterruptionSourceWord
		if d.Source == "vad" {
			source = internal_type.InterruptionSourceVad
		}
		onPacket(ctx, internal_type.InterruptionPacket{ContextID: d.ID, Source: source})

	case TypeClose:
		var d CloseData
		json.Unmarshal(resp.Data, &d)
		onPacket(ctx, internal_type.DirectivePacket{Directive: protos.ConversationDirective_END_CONVERSATION, Arguments: map[string]interface{}{"reason": d.Reason}})

	case TypePing:
		e.send(Request{Type: TypePong, Timestamp: time.Now().UnixMilli()})
	}
}

// mapToolAction maps tool names from websocket to conversation actions.
// func (e *websocketExecutor) mapToolAction(name string) protos.AssistantConversationAction_ActionType {
// 	switch name {
// 	case "disconnect", "end_conversation", "hangup":
// 		return protos.AssistantConversationAction_END_CONVERSATION
// 	default:
// 		return protos.AssistantConversationAction_ACTION_UNSPECIFIED
// 	}
// }

// Execute sends a packet to the WebSocket server.
func (e *websocketExecutor) Execute(ctx context.Context, comm internal_type.Communication, packet internal_type.Packet) error {
	switch p := packet.(type) {
	case internal_type.NormalizedUserTextPacket:
		return e.sendUserMessage(p.ContextID, p.Text)
	case internal_type.UserTextPacket:
		return e.sendUserMessage(p.ContextID, p.Text)
	case internal_type.StaticPacket:
		return nil
	case internal_type.InterruptionPacket:
		e.setCurrentContextID("")
		return nil
	default:
		return fmt.Errorf("unsupported packet: %T", packet)
	}
}

// Close terminates the WebSocket connection.
func (e *websocketExecutor) Close(ctx context.Context) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.conn != nil {
		e.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		e.conn.Close()
		e.conn = nil
	}
	e.contextMu.Lock()
	e.currentID = ""
	e.contextMu.Unlock()
	return nil
}
