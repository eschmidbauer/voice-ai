package internal_agentkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Mock: nopLogger — satisfies commons.Logger with no-ops
// =============================================================================

type nopLogger struct{}

func (nopLogger) Level() zapcore.Level                           { return zapcore.DebugLevel }
func (nopLogger) Debug(...interface{})                           {}
func (nopLogger) Debugf(string, ...interface{})                  {}
func (nopLogger) Debugw(string, ...interface{})                  {}
func (nopLogger) Info(...interface{})                            {}
func (nopLogger) Infof(string, ...interface{})                   {}
func (nopLogger) Infow(string, ...interface{})                   {}
func (nopLogger) Warn(...interface{})                            {}
func (nopLogger) Warnf(string, ...interface{})                   {}
func (nopLogger) Warnw(string, ...interface{})                   {}
func (nopLogger) Error(...interface{})                           {}
func (nopLogger) Errorf(string, ...interface{})                  {}
func (nopLogger) Errorw(string, ...interface{})                  {}
func (nopLogger) DPanic(...interface{})                          {}
func (nopLogger) DPanicf(string, ...interface{})                 {}
func (nopLogger) Panic(...interface{})                           {}
func (nopLogger) Panicf(string, ...interface{})                  {}
func (nopLogger) Fatal(...interface{})                           {}
func (nopLogger) Fatalf(string, ...interface{})                  {}
func (nopLogger) Benchmark(string, time.Duration)                {}
func (nopLogger) Tracef(context.Context, string, ...interface{}) {}
func (nopLogger) Sync() error                                    { return nil }

// =============================================================================
// Mock: packetCollector — thread-safe packet recorder
// =============================================================================

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

func (c *packetCollector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pkts = nil
}

// =============================================================================
// Mock: mockTalker — grpc.BidiStreamingClient[protos.TalkInput, protos.TalkOutput]
// =============================================================================

type recvResult struct {
	out *protos.TalkOutput
	err error
}

type mockTalker struct {
	mu        sync.Mutex
	sendCalls []*protos.TalkInput
	sendErr   error
	recvCh    chan recvResult
	closeSent atomic.Bool
}

func newMockTalker() *mockTalker {
	return &mockTalker{
		recvCh: make(chan recvResult, 16),
	}
}

func (m *mockTalker) Send(req *protos.TalkInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls = append(m.sendCalls, req)
	return m.sendErr
}

func (m *mockTalker) Recv() (*protos.TalkOutput, error) {
	r, ok := <-m.recvCh
	if !ok {
		return nil, io.EOF
	}
	return r.out, r.err
}

func (m *mockTalker) CloseSend() error {
	m.closeSent.Store(true)
	return nil
}

func (m *mockTalker) Header() (metadata.MD, error) { return nil, nil }
func (m *mockTalker) Trailer() metadata.MD         { return nil }
func (m *mockTalker) Context() context.Context     { return context.Background() }
func (m *mockTalker) SendMsg(any) error            { return nil }
func (m *mockTalker) RecvMsg(any) error            { return nil }

// =============================================================================
// Mock: mockCommunication — satisfies internal_type.Communication
// =============================================================================

type mockCommunication struct {
	internal_type.Communication // embedded nil — panics if unoverridden methods called
	collector                   *packetCollector
}

func (m *mockCommunication) OnPacket(ctx context.Context, pkts ...internal_type.Packet) error {
	return m.collector.collect(ctx, pkts...)
}

// =============================================================================
// Helpers
// =============================================================================

func newTestExecutor() *agentkitExecutor {
	return &agentkitExecutor{logger: nopLogger{}}
}

func newTestComm() (*mockCommunication, *packetCollector) {
	c := &packetCollector{}
	return &mockCommunication{collector: c}, c
}

// findPacket returns the first packet of type T from the collector.
func findPacket[T internal_type.Packet](pkts []internal_type.Packet) (T, bool) {
	for _, p := range pkts {
		if v, ok := p.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// findPackets returns all packets of type T from the collector.
func findPackets[T internal_type.Packet](pkts []internal_type.Packet) []T {
	var out []T
	for _, p := range pkts {
		if v, ok := p.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// =============================================================================
// Tests: handleResponse — table-driven, 9 cases
// =============================================================================

func TestHandleResponse(t *testing.T) {
	tests := []struct {
		name     string
		resp     *protos.TalkOutput
		wantFunc func(t *testing.T, pkts []internal_type.Packet)
	}{
		{
			name: "initialization_ack",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Initialization{
					Initialization: &protos.ConversationInitialization{
						AssistantConversationId: 42,
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 1)
				ev, ok := pkts[0].(internal_type.ConversationEventPacket)
				require.True(t, ok)
				assert.Equal(t, "agentkit", ev.Name)
				assert.Equal(t, "initialization_ack", ev.Data["type"])
				assert.Equal(t, "42", ev.Data["conversation_id"])
			},
		},
		{
			name: "interruption",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Interruption{
					Interruption: &protos.ConversationInterruption{Id: "ctx-1"},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 2)
				ip, ok := pkts[0].(internal_type.InterruptionDetectedPacket)
				require.True(t, ok)
				assert.Equal(t, "ctx-1", ip.ContextID)
				assert.Equal(t, internal_type.InterruptionSourceWord, ip.Source)
				ev, ok := pkts[1].(internal_type.ConversationEventPacket)
				require.True(t, ok)
				assert.Equal(t, "interruption", ev.Data["type"])
			},
		},
		{
			name: "text_delta",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Assistant{
					Assistant: &protos.ConversationAssistantMessage{
						Id:        "msg-1",
						Completed: false,
						Message:   &protos.ConversationAssistantMessage_Text{Text: "hello "},
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 2)
				delta, ok := pkts[0].(internal_type.LLMResponseDeltaPacket)
				require.True(t, ok)
				assert.Equal(t, "msg-1", delta.ContextID)
				assert.Equal(t, "hello ", delta.Text)
				ev, ok := pkts[1].(internal_type.ConversationEventPacket)
				require.True(t, ok)
				assert.Equal(t, "agentkit", ev.Name)
				assert.Equal(t, "chunk", ev.Data["type"])
				assert.Equal(t, "hello ", ev.Data["text"])
				assert.Equal(t, "6", ev.Data["response_char_count"])
			},
		},
		{
			name: "text_completed",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Assistant{
					Assistant: &protos.ConversationAssistantMessage{
						Id:        "msg-2",
						Completed: true,
						Message:   &protos.ConversationAssistantMessage_Text{Text: "world"},
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 2)
				done, ok := pkts[0].(internal_type.LLMResponseDonePacket)
				require.True(t, ok)
				assert.Equal(t, "msg-2", done.ContextID)
				assert.Equal(t, "world", done.Text)
				ev, ok := pkts[1].(internal_type.ConversationEventPacket)
				require.True(t, ok)
				assert.Equal(t, "completed", ev.Data["type"])
				assert.Equal(t, "5", ev.Data["response_char_count"])
			},
		},
		{
			name: "audio_noop",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Assistant{
					Assistant: &protos.ConversationAssistantMessage{
						Id:      "msg-3",
						Message: &protos.ConversationAssistantMessage_Audio{Audio: []byte{0x01}},
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				assert.Empty(t, pkts)
			},
		},
		{
			name: "tool_call",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Tool{
					Tool: &protos.ConversationToolCall{
						Id:     "tc-1",
						ToolId: "tool-42",
						Name:   "get_weather",
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 1)
				ev, ok := pkts[0].(internal_type.ConversationEventPacket)
				require.True(t, ok)
				assert.Equal(t, "tool", ev.Name)
				assert.Equal(t, "tool_call", ev.Data["type"])
				assert.Equal(t, "tool-42", ev.Data["tool_id"])
				assert.Equal(t, "get_weather", ev.Data["name"])
			},
		},
		{
			name: "tool_result",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_ToolResult{
					ToolResult: &protos.ConversationToolResult{
						Id:      "tr-1",
						ToolId:  "tool-42",
						Name:    "get_weather",
						Success: true,
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 1)
				ev, ok := pkts[0].(internal_type.ConversationEventPacket)
				require.True(t, ok)
				assert.Equal(t, "tool", ev.Name)
				assert.Equal(t, "tool_result", ev.Data["type"])
				assert.Equal(t, "true", ev.Data["success"])
			},
		},
		{
			name: "error",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Error{
					Error: &protos.Error{
						ErrorCode:    500,
						ErrorMessage: "agent crashed",
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 3)
				errPkt, ok := pkts[0].(internal_type.LLMErrorPacket)
				require.True(t, ok)
				assert.Contains(t, errPkt.Error.Error(), "agent crashed")

				ev, ok := pkts[1].(internal_type.ConversationEventPacket)
				require.True(t, ok)
				assert.Equal(t, "error", ev.Data["type"])
				assert.Equal(t, "500", ev.Data["code"])

				dir, ok := pkts[2].(internal_type.DirectivePacket)
				require.True(t, ok)
				assert.Equal(t, protos.ConversationDirective_END_CONVERSATION, dir.Directive)
			},
		},
		{
			name: "directive",
			resp: &protos.TalkOutput{
				Data: &protos.TalkOutput_Directive{
					Directive: &protos.ConversationDirective{
						Id:   "d-1",
						Type: protos.ConversationDirective_END_CONVERSATION,
					},
				},
			},
			wantFunc: func(t *testing.T, pkts []internal_type.Packet) {
				require.Len(t, pkts, 1)
				dir, ok := pkts[0].(internal_type.DirectivePacket)
				require.True(t, ok)
				assert.Equal(t, "d-1", dir.ContextID)
				assert.Equal(t, protos.ConversationDirective_END_CONVERSATION, dir.Directive)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestExecutor()
			comm, collector := newTestComm()
			e.handleResponse(context.Background(), tt.resp, comm)
			tt.wantFunc(t, collector.all())
		})
	}
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
		{"other", errors.New("something broke"), "something broke"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.streamErrorReason(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// Tests: Execute — 3 cases
// =============================================================================

func TestExecute_UserTextReceivedPacket(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "hello world",
	})

	require.NoError(t, err)

	// Verify ConversationEventPacket emitted
	evs := findPackets[internal_type.ConversationEventPacket](collector.all())
	require.Len(t, evs, 1)
	assert.Equal(t, "executing", evs[0].Data["type"])
	assert.Equal(t, "hello world", evs[0].Data["script"])
	assert.Equal(t, "11", evs[0].Data["input_char_count"])

	// Verify talker.Send was called
	talker.mu.Lock()
	defer talker.mu.Unlock()
	require.Len(t, talker.sendCalls, 1)
	msg := talker.sendCalls[0].GetMessage()
	require.NotNil(t, msg)
	assert.Equal(t, "hello world", msg.GetText())
}

func TestExecute_InjectMessagePacket(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.InjectMessagePacket{
		ContextID: "ctx-1",
		Text:      "static text",
	})

	require.NoError(t, err)
	assert.Empty(t, collector.all(), "InjectMessagePacket should emit no packets")
}

func TestExecute_UnsupportedPacket(t *testing.T) {
	e := newTestExecutor()
	comm, _ := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.EndOfSpeechPacket{ContextID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported packet")
}

// =============================================================================
// Tests: listen lifecycle — 4 cases
// =============================================================================

func TestListen_ContextCancelled(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, _ := newTestComm()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(ctx, comm)
	}()

	select {
	case <-done:
		// success — listener exited
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not exit after context cancellation")
	}
}

func TestListen_NilTalker(t *testing.T) {
	e := newTestExecutor()
	e.talker = nil
	comm, _ := newTestComm()

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(context.Background(), comm)
	}()

	select {
	case <-done:
		// success — exited because talker is nil
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not exit with nil talker")
	}
}

func TestListen_RecvEOF(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	// Push EOF to the recv channel
	talker.recvCh <- recvResult{err: io.EOF}

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

func TestListen_RecvUnavailable(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	talker.recvCh <- recvResult{err: status.Error(codes.Unavailable, "gone")}

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(context.Background(), comm)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not exit on Unavailable")
	}

	dirs := findPackets[internal_type.DirectivePacket](collector.all())
	require.Len(t, dirs, 1)
	assert.Equal(t, "server unavailable", dirs[0].Arguments["reason"])
}

// =============================================================================
// Tests: Close lifecycle — 3 cases
// =============================================================================

func TestClose_Normal(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{})

	// Simulate listener goroutine exiting
	close(e.done)

	err := e.Close(context.Background())
	require.NoError(t, err)
	assert.True(t, talker.closeSent.Load(), "CloseSend should have been called")
}

func TestClose_NilTalkerAndConnection(t *testing.T) {
	e := newTestExecutor()
	e.talker = nil
	e.connection = nil
	e.done = nil

	err := e.Close(context.Background())
	require.NoError(t, err, "Close on nil talker/connection should not panic")
}

func TestClose_FieldsNilAfterClose(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{})
	close(e.done) // listener already done

	_ = e.Close(context.Background())

	e.mu.RLock()
	defer e.mu.RUnlock()
	assert.Nil(t, e.talker, "talker should be nil after Close")
	assert.Nil(t, e.connection, "connection should be nil after Close")
}

// =============================================================================
// Tests: send — 2 cases
// =============================================================================

func TestSend_NilTalker(t *testing.T) {
	e := newTestExecutor()
	e.talker = nil

	err := e.send(&protos.TalkInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestSend_Success(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker

	req := &protos.TalkInput{
		Request: &protos.TalkInput_Message{
			Message: &protos.ConversationUserMessage{
				Message: &protos.ConversationUserMessage_Text{Text: "test"},
			},
		},
	}
	err := e.send(req)
	require.NoError(t, err)

	talker.mu.Lock()
	defer talker.mu.Unlock()
	require.Len(t, talker.sendCalls, 1)
	assert.Equal(t, "test", talker.sendCalls[0].GetMessage().GetText())
}

// =============================================================================
// Tests: Concurrency — 2 cases (run with -race)
// =============================================================================

func TestConcurrency_SendAndClose(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.send(&protos.TalkInput{})
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond) // let some sends happen
		close(e.done)
		_ = e.Close(context.Background())
	}()

	wg.Wait()
	// If no race detected (with -race flag), test passes
}

func TestConcurrency_ListenAndClose(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{})
	comm, _ := newTestComm()

	ctx, cancel := context.WithCancel(context.Background())

	// Start listener
	go func() {
		defer close(e.done)
		e.listen(ctx, comm)
	}()

	// Let listener run briefly then close
	time.Sleep(5 * time.Millisecond)
	cancel()
	err := e.Close(context.Background())
	require.NoError(t, err)
}

// =============================================================================
// Tests: Name
// =============================================================================

func TestName(t *testing.T) {
	e := newTestExecutor()
	assert.Equal(t, "agentkit", e.Name())
}

// =============================================================================
// Tests: Execute with send error
// =============================================================================

func TestExecute_UserTextReceivedPacket_SendError(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	talker.sendErr = fmt.Errorf("connection lost")
	e.talker = talker
	comm, _ := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "hello",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

// =============================================================================
// Tests: listen processes multiple messages before error
// =============================================================================

func TestListen_ProcessesMultipleMessages(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	// Send two deltas, then EOF
	talker.recvCh <- recvResult{out: &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{
			Assistant: &protos.ConversationAssistantMessage{
				Id:      "m1",
				Message: &protos.ConversationAssistantMessage_Text{Text: "hi"},
			},
		},
	}}
	talker.recvCh <- recvResult{out: &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{
			Assistant: &protos.ConversationAssistantMessage{
				Id:      "m1",
				Message: &protos.ConversationAssistantMessage_Text{Text: " there"},
			},
		},
	}}
	talker.recvCh <- recvResult{err: io.EOF}

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

	pkts := collector.all()
	deltas := findPackets[internal_type.LLMResponseDeltaPacket](pkts)
	assert.Len(t, deltas, 2)
	dirs := findPackets[internal_type.DirectivePacket](pkts)
	assert.Len(t, dirs, 1)
}

// =============================================================================
// Tests: Close waits for done channel with timeout
// =============================================================================

func TestClose_WaitsForDoneTimeout(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{}) // never closed — will hit 5s timeout

	start := time.Now()
	// Run Close in background — it will wait up to 5s
	done := make(chan error, 1)
	go func() {
		done <- e.Close(context.Background())
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
		elapsed := time.Since(start)
		// Should take ~5s (timeout), give some slack
		assert.Greater(t, elapsed, 4*time.Second)
	case <-time.After(7 * time.Second):
		t.Fatal("Close did not return within expected timeout")
	}
}

// =============================================================================
// Tests: handleResponse with completed text includes correct contextID
// =============================================================================

func TestHandleResponse_CompletedTextContextID(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{
			Assistant: &protos.ConversationAssistantMessage{
				Id:        "unique-ctx",
				Completed: true,
				Message:   &protos.ConversationAssistantMessage_Text{Text: "done"},
			},
		},
	}
	e.handleResponse(context.Background(), resp, comm)

	pkts := collector.all()
	done, ok := findPacket[internal_type.LLMResponseDonePacket](pkts)
	require.True(t, ok)
	assert.Equal(t, "unique-ctx", done.ContextID)

	ev, ok := findPacket[internal_type.ConversationEventPacket](pkts)
	require.True(t, ok)
	assert.Equal(t, "unique-ctx", ev.ContextID)
}

// =============================================================================
// Tests: tool_result success=false
// =============================================================================

func TestHandleResponse_ToolResultFailed(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.TalkOutput{
		Data: &protos.TalkOutput_ToolResult{
			ToolResult: &protos.ConversationToolResult{
				Id:      "tr-2",
				ToolId:  "tool-99",
				Name:    "calculator",
				Success: false,
			},
		},
	}
	e.handleResponse(context.Background(), resp, comm)

	evs := findPackets[internal_type.ConversationEventPacket](collector.all())
	require.Len(t, evs, 1)
	assert.Equal(t, "false", evs[0].Data["success"])
}

// =============================================================================
// Tests: Execute after Close returns error
// =============================================================================

func TestExecute_AfterClose(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{})
	close(e.done)
	_ = e.Close(context.Background())

	comm, _ := newTestComm()
	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "after close",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

// =============================================================================
// Tests: concurrent send calls are serialized
// =============================================================================

func TestConcurrency_MultipleSends(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker

	var wg sync.WaitGroup
	count := 50
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			_ = e.send(&protos.TalkInput{})
		}()
	}
	wg.Wait()

	talker.mu.Lock()
	defer talker.mu.Unlock()
	assert.Len(t, talker.sendCalls, count)
}

// =============================================================================
// Tests: error packet includes correct error message format
// =============================================================================

func TestHandleResponse_ErrorMessageFormat(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	resp := &protos.TalkOutput{
		Data: &protos.TalkOutput_Error{
			Error: &protos.Error{
				ErrorCode:    403,
				ErrorMessage: "forbidden",
			},
		},
	}
	e.handleResponse(context.Background(), resp, comm)

	errPkts := findPackets[internal_type.LLMErrorPacket](collector.all())
	require.Len(t, errPkts, 1)
	assert.Contains(t, errPkts[0].Error.Error(), "agentkit error 403: forbidden")
}

func TestHandleResponse_StaleContext_Dropped(t *testing.T) {
	e := newTestExecutor()
	e.currentID = "ctx-active"
	comm, collector := newTestComm()

	resp := &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{
			Assistant: &protos.ConversationAssistantMessage{
				Id:        "ctx-stale",
				Completed: true,
				Message:   &protos.ConversationAssistantMessage_Text{Text: "ignore"},
			},
		},
	}
	e.handleResponse(context.Background(), resp, comm)
	assert.Empty(t, collector.all())
}

func TestExecute_InterruptionDetectedPacket_ClearsCurrentContext(t *testing.T) {
	e := newTestExecutor()
	e.currentID = "ctx-1"
	comm, _ := newTestComm()

	err := e.Execute(context.Background(), comm, internal_type.InterruptionDetectedPacket{ContextID: "ctx-1"})
	require.NoError(t, err)
	assert.Equal(t, "", e.currentID)
}

// =============================================================================
// Tests: send error propagation from talker
// =============================================================================

func TestSend_PropagatesTalkerError(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	talker.sendErr = fmt.Errorf("write failed")
	e.talker = talker

	err := e.send(&protos.TalkInput{})
	require.Error(t, err)
	assert.Equal(t, "write failed", err.Error())
}

// =============================================================================
// Concurrent read/write on mu — listener reads, send writes
// =============================================================================

func TestConcurrency_ListenReadSendWrite(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, _ := newTestComm()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(2)

	// Listener: reads from talker
	go func() {
		defer wg.Done()
		e.listen(ctx, comm)
	}()

	// Sender: writes concurrently
	var sendCount atomic.Int32
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = e.send(&protos.TalkInput{})
			sendCount.Add(1)
		}
		// After sending, terminate listener
		talker.recvCh <- recvResult{err: io.EOF}
	}()

	// Wait for listener to exit
	wg.Wait()
	cancel()
	assert.Equal(t, int32(50), sendCount.Load())
}

// =============================================================================
// End-to-End: full conversation flow
// =============================================================================

func TestE2E_FullConversationTurn(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	// 1. User sends a message
	err := e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "turn-1",
		Text:      "What is Go?",
	})
	require.NoError(t, err)

	// Verify: talker received the message, event was emitted
	talker.mu.Lock()
	require.Len(t, talker.sendCalls, 1)
	assert.Equal(t, "What is Go?", talker.sendCalls[0].GetMessage().GetText())
	talker.mu.Unlock()

	evs := findPackets[internal_type.ConversationEventPacket](collector.all())
	require.Len(t, evs, 1)
	assert.Equal(t, "executing", evs[0].Data["type"])

	// 2. Simulate streaming deltas from agent
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "turn-1", Message: &protos.ConversationAssistantMessage_Text{Text: "Go is"},
		}},
	}, comm)
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "turn-1", Message: &protos.ConversationAssistantMessage_Text{Text: " a language"},
		}},
	}, comm)

	// 3. Final response
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "turn-1", Completed: true,
			Message: &protos.ConversationAssistantMessage_Text{Text: "Go is a language"},
		}},
	}, comm)

	pkts := collector.all()
	deltas := findPackets[internal_type.LLMResponseDeltaPacket](pkts)
	dones := findPackets[internal_type.LLMResponseDonePacket](pkts)
	assert.Len(t, deltas, 2)
	assert.Equal(t, "Go is", deltas[0].Text)
	assert.Equal(t, " a language", deltas[1].Text)
	require.Len(t, dones, 1)
	assert.Equal(t, "Go is a language", dones[0].Text)
}

func TestE2E_MultiTurnConversation(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	for turn := 1; turn <= 5; turn++ {
		ctxID := fmt.Sprintf("turn-%d", turn)
		_ = e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
			ContextID: ctxID, Text: fmt.Sprintf("msg-%d", turn),
		})
		e.handleResponse(context.Background(), &protos.TalkOutput{
			Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
				Id: ctxID, Completed: true,
				Message: &protos.ConversationAssistantMessage_Text{Text: fmt.Sprintf("reply-%d", turn)},
			}},
		}, comm)
	}

	dones := findPackets[internal_type.LLMResponseDonePacket](collector.all())
	assert.Len(t, dones, 5)
	for i, d := range dones {
		assert.Equal(t, fmt.Sprintf("reply-%d", i+1), d.Text)
	}

	talker.mu.Lock()
	assert.Len(t, talker.sendCalls, 5)
	talker.mu.Unlock()
}

func TestE2E_InterruptDuringStreaming(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	// User sends
	_ = e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1", Text: "tell me a story",
	})

	// Delta arrives
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "ctx-1", Message: &protos.ConversationAssistantMessage_Text{Text: "Once upon"},
		}},
	}, comm)

	// Interrupt
	_ = e.Execute(context.Background(), comm, internal_type.InterruptionDetectedPacket{ContextID: "ctx-1"})
	assert.Equal(t, "", e.currentID)

	// New context
	_ = e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-2", Text: "new topic",
	})

	// Stale delta from ctx-1 — rejected (currentID="ctx-2")
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "ctx-1", Completed: true,
			Message: &protos.ConversationAssistantMessage_Text{Text: "stale"},
		}},
	}, comm)

	dones := findPackets[internal_type.LLMResponseDonePacket](collector.all())
	assert.Empty(t, dones, "stale completed response should be dropped")
}

func TestE2E_ToolCallAndResult(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()
	e.currentID = "ctx-1"

	// Tool call
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Tool{Tool: &protos.ConversationToolCall{
			Id: "ctx-1", ToolId: "tool-1", Name: "get_weather",
		}},
	}, comm)

	// Tool result
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_ToolResult{ToolResult: &protos.ConversationToolResult{
			Id: "ctx-1", ToolId: "tool-1", Name: "get_weather", Success: true,
		}},
	}, comm)

	// Final response after tool
	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "ctx-1", Completed: true,
			Message: &protos.ConversationAssistantMessage_Text{Text: "It's 20C"},
		}},
	}, comm)

	pkts := collector.all()
	events := findPackets[internal_type.ConversationEventPacket](pkts)
	toolEvents := make([]string, 0)
	for _, ev := range events {
		if ev.Name == "tool" {
			toolEvents = append(toolEvents, ev.Data["type"])
		}
	}
	assert.Equal(t, []string{"tool_call", "tool_result"}, toolEvents)

	dones := findPackets[internal_type.LLMResponseDonePacket](pkts)
	require.Len(t, dones, 1)
	assert.Equal(t, "It's 20C", dones[0].Text)
}

func TestE2E_ErrorEndsConversation(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	e.handleResponse(context.Background(), &protos.TalkOutput{
		Data: &protos.TalkOutput_Error{Error: &protos.Error{
			ErrorCode: 500, ErrorMessage: "agent crashed",
		}},
	}, comm)

	pkts := collector.all()
	errPkts := findPackets[internal_type.LLMErrorPacket](pkts)
	dirs := findPackets[internal_type.DirectivePacket](pkts)
	require.Len(t, errPkts, 1)
	assert.Contains(t, errPkts[0].Error.Error(), "agent crashed")
	require.Len(t, dirs, 1)
	assert.Equal(t, protos.ConversationDirective_END_CONVERSATION, dirs[0].Directive)
}

func TestE2E_ListenProcessesAndExitsOnEOF(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, collector := newTestComm()

	// Queue: delta, completed, EOF
	talker.recvCh <- recvResult{out: &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "r1", Message: &protos.ConversationAssistantMessage_Text{Text: "chunk"},
		}},
	}}
	talker.recvCh <- recvResult{out: &protos.TalkOutput{
		Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "r1", Completed: true,
			Message: &protos.ConversationAssistantMessage_Text{Text: "done"},
		}},
	}}
	talker.recvCh <- recvResult{err: io.EOF}

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.listen(context.Background(), comm)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("listen did not exit")
	}

	pkts := collector.all()
	deltas := findPackets[internal_type.LLMResponseDeltaPacket](pkts)
	dones := findPackets[internal_type.LLMResponseDonePacket](pkts)
	dirs := findPackets[internal_type.DirectivePacket](pkts)
	assert.Len(t, deltas, 1)
	assert.Len(t, dones, 1)
	assert.Len(t, dirs, 1)
}

// =============================================================================
// Deadlock Detection (run with -timeout 10s and -race)
// =============================================================================

func TestDeadlock_ExecuteAndResponseConcurrent(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, _ := newTestComm()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = e.Execute(ctx, comm, internal_type.NormalizedUserTextPacket{
				ContextID: fmt.Sprintf("ctx-%d", i),
				Text:      fmt.Sprintf("msg-%d", i),
			})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			e.handleResponse(ctx, &protos.TalkOutput{
				Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
					Id: fmt.Sprintf("ctx-%d", i), Completed: true,
					Message: &protos.ConversationAssistantMessage_Text{Text: "resp"},
				}},
			}, comm)
		}
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("DEADLOCK: Execute + handleResponse timed out")
	}
}

func TestDeadlock_ListenAndExecuteAndClose(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{})
	comm, _ := newTestComm()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)

	// Listener
	go func() {
		defer close(e.done)
		defer wg.Done()
		e.listen(ctx, comm)
	}()

	// Execute
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_ = e.Execute(ctx, comm, internal_type.NormalizedUserTextPacket{
				ContextID: fmt.Sprintf("ctx-%d", i),
				Text:      fmt.Sprintf("msg-%d", i),
			})
		}
	}()

	// Close (after brief delay, unblock listener)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		cancel() // cancel context to unblock listener
		close(talker.recvCh)
		time.Sleep(time.Millisecond)
		// Reset done so Close doesn't wait on already-closed channel
		e.mu.Lock()
		e.done = nil
		e.mu.Unlock()
		_ = e.Close(context.Background())
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("DEADLOCK: listen + Execute + Close timed out")
	}
}

// =============================================================================
// Concurrency: race detector stress tests (run with -race)
// =============================================================================

func TestConcurrency_ExecuteAndInterruptRace(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, _ := newTestComm()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
				ContextID: fmt.Sprintf("ctx-%d", i),
				Text:      fmt.Sprintf("msg-%d", i),
			})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.Execute(context.Background(), comm, internal_type.InterruptionDetectedPacket{
				ContextID: fmt.Sprintf("ctx-%d", i),
			})
		}
	}()

	wg.Wait()
}

func TestConcurrency_ResponseAndInterruptRace(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	comm, _ := newTestComm()

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.Execute(context.Background(), comm, internal_type.NormalizedUserTextPacket{
				ContextID: fmt.Sprintf("ctx-%d", i),
				Text:      fmt.Sprintf("msg-%d", i),
			})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			e.handleResponse(context.Background(), &protos.TalkOutput{
				Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
					Id:      fmt.Sprintf("ctx-%d", i),
					Message: &protos.ConversationAssistantMessage_Text{Text: "resp"},
				}},
			}, comm)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.Execute(context.Background(), comm, internal_type.InterruptionDetectedPacket{
				ContextID: fmt.Sprintf("ctx-%d", i),
			})
		}
	}()

	wg.Wait()
}

// =============================================================================
// Consistency
// =============================================================================

func TestConsistency_StaleContextDoesNotEmitPackets(t *testing.T) {
	e := newTestExecutor()
	comm, collector := newTestComm()

	e.setCurrentContextID("ctx-active")

	// Stale responses should not emit
	staleTypes := []*protos.TalkOutput{
		{Data: &protos.TalkOutput_Assistant{Assistant: &protos.ConversationAssistantMessage{
			Id: "ctx-stale", Completed: true,
			Message: &protos.ConversationAssistantMessage_Text{Text: "ignore"},
		}}},
		{Data: &protos.TalkOutput_Interruption{Interruption: &protos.ConversationInterruption{Id: "ctx-stale"}}},
		{Data: &protos.TalkOutput_Tool{Tool: &protos.ConversationToolCall{Id: "ctx-stale"}}},
		{Data: &protos.TalkOutput_ToolResult{ToolResult: &protos.ConversationToolResult{Id: "ctx-stale"}}},
		{Data: &protos.TalkOutput_Directive{Directive: &protos.ConversationDirective{Id: "ctx-stale", Type: protos.ConversationDirective_END_CONVERSATION}}},
	}

	for _, resp := range staleTypes {
		e.handleResponse(context.Background(), resp, comm)
	}

	assert.Empty(t, collector.all(), "all stale context responses should be dropped")
}

func TestConsistency_CloseResetsCurrentID(t *testing.T) {
	e := newTestExecutor()
	talker := newMockTalker()
	e.talker = talker
	e.done = make(chan struct{})
	close(e.done)
	e.currentID = "active"

	_ = e.Close(context.Background())

	e.mu.RLock()
	defer e.mu.RUnlock()
	assert.Equal(t, "", e.currentID)
	assert.Nil(t, e.talker)
}
