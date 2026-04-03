// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_asterisk

import (
	"encoding/json"
	"strconv"
	"strings"
)

// AsteriskMediaEvent represents media events from Asterisk WebSocket
// Based on chan_websocket protocol from Asterisk
type AsteriskMediaEvent struct {
	// Event type: MEDIA_START, MEDIA_STOP, MEDIA_XON, MEDIA_XOFF, MEDIA_BUFFERING_COMPLETED, etc.
	Event string `json:"event,omitempty"`

	// Command type for JSON mode: START_MEDIA_BUFFERING, STOP_MEDIA_BUFFERING, MARK_MEDIA, HANGUP
	Command string `json:"command,omitempty"`

	// Channel name (populated in MEDIA_START)
	Channel string `json:"channel,omitempty"`

	// Optimal frame size for audio chunks
	OptimalFrameSize int `json:"optimal_frame_size,omitempty"`

	// Correlation ID for tracking
	CorrelationID string `json:"correlation_id,omitempty"`

	// Raw message for non-JSON text messages
	RawMessage string `json:"-"`
}

// ParseAsteriskEvent parses an Asterisk event from string or JSON
func ParseAsteriskEvent(data string) (*AsteriskMediaEvent, error) {
	event := &AsteriskMediaEvent{}
	if err := json.Unmarshal([]byte(data), event); err == nil && (event.Event != "" || event.Command != "") {
		return event, nil
	}
	event.RawMessage = data
	event.Event = parseEventType(data)
	params := parseKeyValuePairs(data)
	if v, ok := params["channel"]; ok {
		event.Channel = v
	}
	if v, ok := params["optimal_frame_size"]; ok {
		if size, err := strconv.Atoi(v); err == nil {
			event.OptimalFrameSize = size
		}
	}
	return event, nil
}

// parseEventType extracts the event type from a space-separated message
func parseEventType(data string) string {
	for i, c := range data {
		if c == ' ' {
			return data[:i]
		}
	}
	return data
}

// parseKeyValuePairs parses "key:value" pairs from a space-separated string
func parseKeyValuePairs(data string) map[string]string {
	result := make(map[string]string)
	parts := strings.Fields(data)

	for _, part := range parts[1:] { // Skip the event type
		for i, c := range part {
			if c == ':' {
				key := part[:i]
				value := part[i+1:]
				result[key] = value
				break
			}
		}
	}

	return result
}

// AsteriskARIEvent represents an ARI event from Asterisk
type AsteriskARIEvent struct {
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	RequestID string                 `json:"request_id,omitempty"`
	Channel   *AsteriskChannel       `json:"channel,omitempty"`
	Bridge    *AsteriskBridge        `json:"bridge,omitempty"`
	Peer      *AsteriskChannel       `json:"peer,omitempty"`
	Extra     map[string]interface{} `json:"-"`
}

// AsteriskChannel represents a channel in ARI
type AsteriskChannel struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	State       string            `json:"state"`
	Caller      *AsteriskEndpoint `json:"caller,omitempty"`
	Connected   *AsteriskEndpoint `json:"connected,omitempty"`
	Dialplan    *AsteriskDialplan `json:"dialplan,omitempty"`
	ChannelVars map[string]string `json:"channelvars,omitempty"`
}

// AsteriskEndpoint represents caller/connected endpoint info
type AsteriskEndpoint struct {
	Name   string `json:"name"`
	Number string `json:"number"`
}

// AsteriskDialplan represents dialplan context
type AsteriskDialplan struct {
	Context string `json:"context"`
	Exten   string `json:"exten"`
	AppData string `json:"app_data"`
}

// AsteriskBridge represents a bridge in ARI
type AsteriskBridge struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	BridgeType string   `json:"bridge_type"`
	Channels   []string `json:"channels"`
}

// AsteriskRESTRequest represents a REST request over WebSocket
type AsteriskRESTRequest struct {
	Type         string              `json:"type"`
	RequestID    string              `json:"request_id"`
	Method       string              `json:"method"`
	URI          string              `json:"uri"`
	QueryStrings []map[string]string `json:"query_strings,omitempty"`
}

// AsteriskRESTResponse represents a REST response over WebSocket
type AsteriskRESTResponse struct {
	Type         string `json:"type"`
	RequestID    string `json:"request_id"`
	StatusCode   int    `json:"status_code"`
	ReasonPhrase string `json:"reason_phrase"`
	MessageBody  string `json:"message_body,omitempty"`
}
