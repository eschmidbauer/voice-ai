package internal_websocket

import (
	"context"
	"testing"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestExecutor(t *testing.T) *websocketExecutor {
	t.Helper()
	logger, _ := commons.NewApplicationLogger()
	return &websocketExecutor{logger: logger}
}

func TestHandleResponse_Stream_StaleContextDropped(t *testing.T) {
	e := newTestExecutor(t)
	e.currentID = "ctx-active"

	collected := make([]internal_type.Packet, 0)
	e.handleResponse(context.Background(), &Response{
		Type: TypeStream,
		Data: []byte(`{"id":"ctx-stale","content":"hello","index":0}`),
	}, func(_ context.Context, packet ...internal_type.Packet) error {
		collected = append(collected, packet...)
		return nil
	})

	assert.Empty(t, collected)
}

func TestHandleResponse_Stream_CurrentContextEmits(t *testing.T) {
	e := newTestExecutor(t)
	e.currentID = "ctx-1"

	collected := make([]internal_type.Packet, 0)
	e.handleResponse(context.Background(), &Response{
		Type: TypeStream,
		Data: []byte(`{"id":"ctx-1","content":"hello","index":0}`),
	}, func(_ context.Context, packet ...internal_type.Packet) error {
		collected = append(collected, packet...)
		return nil
	})

	require.Len(t, collected, 1)
	delta, ok := collected[0].(internal_type.LLMResponseDeltaPacket)
	require.True(t, ok)
	assert.Equal(t, "ctx-1", delta.ContextID)
	assert.Equal(t, "hello", delta.Text)
}

func TestExecute_NormalizedUserTextPacket_EmptyContextNoop(t *testing.T) {
	e := newTestExecutor(t)
	err := e.Execute(context.Background(), nil, internal_type.NormalizedUserTextPacket{Text: "hello"})
	require.NoError(t, err)
	assert.Equal(t, "", e.currentID)
}

func TestExecute_InterruptionPacket_ClearsContext(t *testing.T) {
	e := newTestExecutor(t)
	e.currentID = "ctx-1"
	err := e.Execute(context.Background(), nil, internal_type.InterruptionPacket{ContextID: "ctx-1"})
	require.NoError(t, err)
	assert.Equal(t, "", e.currentID)
}

func TestExecute_UnsupportedPacket(t *testing.T) {
	e := newTestExecutor(t)
	err := e.Execute(context.Background(), nil, internal_type.EndOfSpeechPacket{ContextID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported packet")
}
