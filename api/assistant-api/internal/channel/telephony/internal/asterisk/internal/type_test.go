// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_asterisk

import (
	"testing"
)

func TestParseAsteriskEvent_JSONEvent(t *testing.T) {
	data := `{"event":"MEDIA_START","channel":"SIP/test-001","optimal_frame_size":160}`
	event, err := ParseAsteriskEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Event != "MEDIA_START" {
		t.Errorf("expected MEDIA_START, got %q", event.Event)
	}
	if event.Channel != "SIP/test-001" {
		t.Errorf("expected SIP/test-001, got %q", event.Channel)
	}
	if event.OptimalFrameSize != 160 {
		t.Errorf("expected 160, got %d", event.OptimalFrameSize)
	}
}

func TestParseAsteriskEvent_JSONCommand(t *testing.T) {
	data := `{"command":"HANGUP"}`
	event, err := ParseAsteriskEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Command != "HANGUP" {
		t.Errorf("expected HANGUP, got %q", event.Command)
	}
}

func TestParseAsteriskEvent_PlainText(t *testing.T) {
	data := "MEDIA_START channel:SIP/test-002 optimal_frame_size:320"
	event, err := ParseAsteriskEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Event != "MEDIA_START" {
		t.Errorf("expected MEDIA_START, got %q", event.Event)
	}
	if event.Channel != "SIP/test-002" {
		t.Errorf("expected SIP/test-002, got %q", event.Channel)
	}
	if event.OptimalFrameSize != 320 {
		t.Errorf("expected 320, got %d", event.OptimalFrameSize)
	}
}

func TestParseAsteriskEvent_PlainTextNoParams(t *testing.T) {
	data := "MEDIA_STOP"
	event, err := ParseAsteriskEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Event != "MEDIA_STOP" {
		t.Errorf("expected MEDIA_STOP, got %q", event.Event)
	}
	if event.RawMessage != "MEDIA_STOP" {
		t.Errorf("expected raw message, got %q", event.RawMessage)
	}
}

func TestParseAsteriskEvent_PlainTextWithExtraSpaces(t *testing.T) {
	data := "MEDIA_START  channel:SIP/test   optimal_frame_size:256"
	event, err := ParseAsteriskEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Event != "MEDIA_START" {
		t.Errorf("expected MEDIA_START, got %q", event.Event)
	}
	if event.Channel != "SIP/test" {
		t.Errorf("expected SIP/test, got %q", event.Channel)
	}
	if event.OptimalFrameSize != 256 {
		t.Errorf("expected 256, got %d", event.OptimalFrameSize)
	}
}

func TestParseAsteriskEvent_InvalidOptimalFrameSize(t *testing.T) {
	data := "MEDIA_START optimal_frame_size:abc"
	event, err := ParseAsteriskEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.OptimalFrameSize != 0 {
		t.Errorf("expected 0 for invalid frame size, got %d", event.OptimalFrameSize)
	}
}

func TestParseAsteriskEvent_EmptyJSON(t *testing.T) {
	data := `{}`
	event, err := ParseAsteriskEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty JSON has no event or command, so it falls back to plain-text parsing
	if event.RawMessage != `{}` {
		t.Errorf("expected raw message for empty JSON, got %q", event.RawMessage)
	}
}
