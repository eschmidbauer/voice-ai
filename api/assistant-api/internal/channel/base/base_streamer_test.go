// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package channel_base

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test helpers
// ============================================================================

// defaultTestOpts returns options that produce the same thresholds as the
// old defaultTestConfig (µ-law 8kHz: 480 byte input threshold, 160 byte frames).
func defaultTestOpts() []Option {
	return []Option{
		WithInputChannelSize(10),
		WithOutputChannelSize(10),
		WithInputBufferThreshold(480),  // 8kHz * 60ms
		WithOutputBufferThreshold(480), // same as 3 frames
		WithOutputFrameSize(160),       // 8kHz * 20ms
	}
}

func newTestStreamer() (*BaseStreamer, commons.Logger) {
	logger, _ := commons.NewApplicationLogger()
	bs := NewBaseStreamer(logger, defaultTestOpts()...)
	return &bs, logger
}

// ============================================================================
// NewBaseStreamer
// ============================================================================

func TestNewBaseStreamer_Initialisation(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()

	bs := NewBaseStreamer(logger,
		WithInputChannelSize(5),
		WithOutputChannelSize(8),
		WithInputBufferThreshold(100),
		WithOutputBufferThreshold(200),
		WithOutputFrameSize(50),
	)

	assert.NotNil(t, bs.Logger, "Logger should be set")
	assert.NotNil(t, bs.Ctx, "Context should be set")
	assert.NotNil(t, bs.Cancel, "CancelFunc should be set")
	assert.False(t, bs.Closed, "Closed should start as false")

	// Channel capacities
	assert.Equal(t, 5, cap(bs.InputCh), "InputCh capacity should match config")
	assert.Equal(t, 8, cap(bs.OutputCh), "OutputCh capacity should match config")
	assert.Equal(t, 1, cap(bs.FlushAudioCh), "FlushAudioCh should have capacity 1")

	// Config accessors
	assert.Equal(t, 100, bs.InputBufferThreshold(), "InputBufferThreshold should match option")
	assert.Equal(t, 200, bs.OutputBufferThreshold(), "OutputBufferThreshold should match option")
	assert.Equal(t, 50, bs.OutputFrameSize(), "OutputFrameSize should match option")

	// Context should not be cancelled
	select {
	case <-bs.Ctx.Done():
		t.Fatal("Context should not be cancelled on creation")
	default:
	}
}

func TestNewBaseStreamer_Defaults(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	bs := NewBaseStreamer(logger) // no options — all defaults

	assert.NotNil(t, bs.InputCh)
	assert.NotNil(t, bs.OutputCh)
	assert.NotNil(t, bs.FlushAudioCh)

	// Default channel sizes
	assert.Equal(t, DefaultInputChannelSize, cap(bs.InputCh), "InputCh should use DefaultInputChannelSize")
	assert.Equal(t, DefaultOutputChannelSize, cap(bs.OutputCh), "OutputCh should use DefaultOutputChannelSize")
}

func TestNewBaseStreamer_AudioConfigDerived(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()

	// µ-law 8kHz mono: 8 bytes/ms
	mulaw8k := &protos.AudioConfig{
		SampleRate:  8000,
		AudioFormat: protos.AudioConfig_MuLaw8,
		Channels:    1,
	}

	bs := NewBaseStreamer(logger,
		WithInputAudioConfig(mulaw8k),
		WithOutputAudioConfig(mulaw8k),
	)

	// Input: 8 bytes/ms × 80ms = 640
	assert.Equal(t, 640, bs.InputBufferThreshold(), "Should derive input threshold from audio config")
	// Output: 8 bytes/ms × 20ms = 160
	assert.Equal(t, 160, bs.OutputFrameSize(), "Should derive output frame size from audio config")
	// Output threshold defaults to frame size
	assert.Equal(t, 160, bs.OutputBufferThreshold(), "Output threshold should default to frame size")
}

func TestNewBaseStreamer_ExplicitOverridesAudioConfig(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()

	mulaw8k := &protos.AudioConfig{
		SampleRate:  8000,
		AudioFormat: protos.AudioConfig_MuLaw8,
		Channels:    1,
	}

	bs := NewBaseStreamer(logger,
		WithInputAudioConfig(mulaw8k),
		WithOutputAudioConfig(mulaw8k),
		WithInputBufferThreshold(999),
		WithOutputFrameSize(111),
		WithOutputBufferThreshold(222),
	)

	// Explicit values should override derived values
	assert.Equal(t, 999, bs.InputBufferThreshold())
	assert.Equal(t, 111, bs.OutputFrameSize())
	assert.Equal(t, 222, bs.OutputBufferThreshold())
}

// ============================================================================
// Context
// ============================================================================

func TestContext_ReturnsStreamerContext(t *testing.T) {
	bs, _ := newTestStreamer()
	assert.Equal(t, bs.Ctx, bs.Context())
}

func TestContext_CancelledAfterCancel(t *testing.T) {
	bs, _ := newTestStreamer()
	bs.Cancel()

	select {
	case <-bs.Context().Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after Cancel()")
	}
}

// ============================================================================
// Input
// ============================================================================

func TestInput_SendsNormalMessage(t *testing.T) {
	bs, _ := newTestStreamer()
	msg := &protos.ConversationUserMessage{
		Message: &protos.ConversationUserMessage_Audio{Audio: []byte{1, 2, 3}},
	}

	bs.Input(msg)

	select {
	case got := <-bs.InputCh:
		assert.Equal(t, msg, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected message on InputCh")
	}
}

func TestInput_RoutesEventToLow(t *testing.T) {
	bs, _ := newTestStreamer()
	msg := &protos.ConversationEvent{Name: "test"}

	bs.Input(msg)

	select {
	case got := <-bs.LowCh:
		assert.Equal(t, msg, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected message on LowCh")
	}
}

func TestInput_RoutesDisconnectionToCritical(t *testing.T) {
	bs, _ := newTestStreamer()
	msg := &protos.ConversationDisconnection{Type: protos.ConversationDisconnection_DISCONNECTION_TYPE_USER}

	bs.Input(msg)

	select {
	case got := <-bs.CriticalCh:
		assert.Equal(t, msg, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected message on CriticalCh")
	}
}

func TestInput_DropsWhenFull(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	bs := NewBaseStreamer(logger,
		WithInputChannelSize(1),
		WithOutputChannelSize(1),
	)

	msg1 := &protos.ConversationUserMessage{}
	msg2 := &protos.ConversationUserMessage{}

	bs.Input(msg1) // fills the buffer
	bs.Input(msg2) // should be dropped (non-blocking)
}

// ============================================================================
// PushOutput
// ============================================================================

func TestPushOutput_SendsMessage(t *testing.T) {
	bs, _ := newTestStreamer()
	msg := &protos.ConversationAssistantMessage{
		Message: &protos.ConversationAssistantMessage_Audio{Audio: []byte{4, 5}},
	}

	bs.Output(msg)

	select {
	case got := <-bs.OutputCh:
		assert.Equal(t, msg, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected message on OutputCh")
	}
}

// ============================================================================
// Recv
// ============================================================================

func TestRecv_ReturnsMessageFromInputCh(t *testing.T) {
	bs, _ := newTestStreamer()
	msg := &protos.ConversationUserMessage{}
	bs.InputCh <- msg

	got, err := bs.Recv()
	require.NoError(t, err)
	assert.Equal(t, msg, got)
}

func TestRecv_ReturnsEOFOnContextCancel(t *testing.T) {
	bs, _ := newTestStreamer()
	bs.Cancel()

	got, err := bs.Recv()
	assert.Nil(t, got)
	assert.Equal(t, io.EOF, err)
}

func TestRecv_ReturnsEOFOnChannelClose(t *testing.T) {
	bs, _ := newTestStreamer()
	close(bs.InputCh)

	got, err := bs.Recv()
	assert.Nil(t, got)
	assert.Equal(t, io.EOF, err)
}

func TestRecv_BlocksUntilMessageAvailable(t *testing.T) {
	bs, _ := newTestStreamer()
	msg := &protos.ConversationUserMessage{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := bs.Recv()
		require.NoError(t, err)
		assert.Equal(t, msg, got)
	}()

	// Give the goroutine time to block
	time.Sleep(20 * time.Millisecond)

	bs.InputCh <- msg

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Recv should have unblocked")
	}
}

// ============================================================================
// BufferAndSendInput
// ============================================================================

func TestBufferAndSendInput_BuffersUntilThreshold(t *testing.T) {
	bs, _ := newTestStreamer()

	// Send less than threshold (480 bytes)
	chunk := make([]byte, 200)
	bs.BufferAndSendInput(chunk)

	// Nothing should be on the channel yet
	select {
	case <-bs.InputCh:
		t.Fatal("Should not send before reaching threshold")
	default:
	}
}

func TestBufferAndSendInput_FlushesAtThreshold(t *testing.T) {
	bs, _ := newTestStreamer()
	threshold := 480

	// Send exactly the threshold
	chunk := make([]byte, threshold)
	for i := range chunk {
		chunk[i] = byte(i % 256)
	}
	bs.BufferAndSendInput(chunk)

	select {
	case msg := <-bs.InputCh:
		audio := msg.(*protos.ConversationUserMessage).GetAudio()
		assert.Equal(t, threshold, len(audio), "Should flush all buffered audio at threshold")
		assert.Equal(t, chunk, audio, "Audio data should match")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected flushed message on InputCh")
	}
}

func TestBufferAndSendInput_AccumulatesMultipleChunks(t *testing.T) {
	bs, _ := newTestStreamer()

	// Send two chunks that together exceed threshold
	bs.BufferAndSendInput(make([]byte, 300))
	bs.BufferAndSendInput(make([]byte, 300)) // total=600 > 480

	select {
	case msg := <-bs.InputCh:
		audio := msg.(*protos.ConversationUserMessage).GetAudio()
		assert.Equal(t, 600, len(audio), "Should flush all buffered audio when exceeding threshold")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected flushed message on InputCh")
	}
}

// ============================================================================
// BufferAndSendOutput
// ============================================================================

func TestBufferAndSendOutput_BuffersUntilThreshold(t *testing.T) {
	bs, _ := newTestStreamer()

	// Send less than threshold (480 bytes)
	bs.BufferAndSendOutput(make([]byte, 100))

	select {
	case <-bs.OutputCh:
		t.Fatal("Should not send before reaching threshold")
	default:
	}
}

func TestBufferAndSendOutput_ProducesCorrectFrameSize(t *testing.T) {
	bs, _ := newTestStreamer()
	frameSize := 160

	// Send enough data for 3 full frames (3 * 160 = 480 = threshold)
	data := make([]byte, 480)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bs.BufferAndSendOutput(data)

	// Should produce 3 frames of 160 bytes each
	for i := 0; i < 3; i++ {
		select {
		case msg := <-bs.OutputCh:
			audio := msg.(*protos.ConversationAssistantMessage).GetAudio()
			assert.Equal(t, frameSize, len(audio), "Each frame should be OutputFrameSize bytes")
			// Verify frame data
			expected := data[i*frameSize : (i+1)*frameSize]
			assert.Equal(t, expected, audio, "Frame %d data should match", i)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Expected frame %d on OutputCh", i)
		}
	}
}

func TestBufferAndSendOutput_RetainsPartialFrame(t *testing.T) {
	bs, _ := newTestStreamer()
	// 500 bytes = 3 full frames (480) + 20 bytes remainder
	bs.BufferAndSendOutput(make([]byte, 500))

	// Drain the 3 frames
	for i := 0; i < 3; i++ {
		select {
		case <-bs.OutputCh:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Expected frame %d", i)
		}
	}

	// No more frames should be available
	select {
	case <-bs.OutputCh:
		t.Fatal("Should not produce a partial frame")
	default:
	}

	// The remaining 20 bytes should still be in the buffer.
	// Send another 460 bytes to reach 480 (threshold) and produce 3 more frames.
	bs.BufferAndSendOutput(make([]byte, 460))

	framesReceived := 0
	for {
		select {
		case <-bs.OutputCh:
			framesReceived++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, 3, framesReceived, "Remainder + new data should produce 3 frames")
}

// ============================================================================
// ClearInputBuffer
// ============================================================================

func TestClearInputBuffer_ResetsBuffer(t *testing.T) {
	bs, _ := newTestStreamer()

	// Partially fill the input buffer (below threshold)
	bs.BufferAndSendInput(make([]byte, 100))

	bs.ClearInputBuffer()

	// After clearing, sending more data should start from zero — not accumulate with old data.
	bs.BufferAndSendInput(make([]byte, 100))

	select {
	case <-bs.InputCh:
		t.Fatal("Should not flush: only 100 bytes (not 200) after clear")
	default:
		// expected
	}
}

func TestClearInputBuffer_DrainsChannel(t *testing.T) {
	bs, _ := newTestStreamer()

	// Put some messages on the channel
	bs.InputCh <- &protos.ConversationUserMessage{}
	bs.InputCh <- &protos.ConversationUserMessage{}

	bs.ClearInputBuffer()

	select {
	case <-bs.InputCh:
		t.Fatal("InputCh should be drained after ClearInputBuffer")
	default:
		// expected
	}
}

// ============================================================================
// ClearOutputBuffer
// ============================================================================

func TestClearOutputBuffer_ResetsBuffer(t *testing.T) {
	bs, _ := newTestStreamer()

	// Partially fill output buffer
	bs.BufferAndSendOutput(make([]byte, 100))

	bs.ClearOutputBuffer()

	// After clearing, accumulation should restart from zero.
	bs.BufferAndSendOutput(make([]byte, 100))

	select {
	case <-bs.OutputCh:
		t.Fatal("Should not flush: only 100 bytes after clear, not 200")
	default:
	}
}

func TestClearOutputBuffer_DrainsChannel(t *testing.T) {
	bs, _ := newTestStreamer()

	bs.OutputCh <- &protos.ConversationAssistantMessage{}
	bs.OutputCh <- &protos.ConversationAssistantMessage{}

	bs.ClearOutputBuffer()

	select {
	case <-bs.OutputCh:
		t.Fatal("OutputCh should be drained after ClearOutputBuffer")
	default:
	}
}

func TestClearOutputBuffer_SignalsFlushAudioCh(t *testing.T) {
	bs, _ := newTestStreamer()

	bs.ClearOutputBuffer()

	select {
	case <-bs.FlushAudioCh:
		// expected
	default:
		t.Fatal("ClearOutputBuffer should signal FlushAudioCh")
	}
}

// ============================================================================
// WithInputBuffer / WithOutputBuffer (synchronous helpers)
// ============================================================================

func TestWithInputBuffer_HoldsLock(t *testing.T) {
	bs, _ := newTestStreamer()

	var calledWithBuf bool
	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		calledWithBuf = buf != nil
		buf.Write([]byte("test-data"))
	})

	assert.True(t, calledWithBuf, "Callback should receive non-nil buffer")

	// Verify the data was written
	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		assert.Equal(t, "test-data", buf.String())
	})
}

func TestWithOutputBuffer_HoldsLock(t *testing.T) {
	bs, _ := newTestStreamer()

	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		buf.Write([]byte("output-data"))
	})

	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		assert.Equal(t, "output-data", buf.String())
	})
}

func TestWithInputBuffer_ConcurrentAccess(t *testing.T) {
	bs, _ := newTestStreamer()
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bs.WithInputBuffer(func(buf *bytes.Buffer) {
				buf.Write([]byte("x"))
			})
		}()
	}

	wg.Wait()

	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		assert.Equal(t, iterations, buf.Len(), "All concurrent writes should succeed")
	})
}

func TestWithOutputBuffer_ConcurrentAccess(t *testing.T) {
	bs, _ := newTestStreamer()
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bs.WithOutputBuffer(func(buf *bytes.Buffer) {
				buf.Write([]byte("y"))
			})
		}()
	}

	wg.Wait()

	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		assert.Equal(t, iterations, buf.Len(), "All concurrent writes should succeed")
	})
}

// ============================================================================
// ResetInputBuffer / ResetOutputBuffer
// ============================================================================

func TestResetInputBuffer(t *testing.T) {
	bs, _ := newTestStreamer()

	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		buf.Write([]byte("some-data"))
	})

	bs.ResetInputBuffer()

	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		assert.Equal(t, 0, buf.Len(), "Buffer should be empty after reset")
	})
}

func TestResetOutputBuffer(t *testing.T) {
	bs, _ := newTestStreamer()

	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		buf.Write([]byte("some-output"))
	})

	bs.ResetOutputBuffer()

	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		assert.Equal(t, 0, buf.Len(), "Buffer should be empty after reset")
	})
}

// ============================================================================
// Disconnect
// ============================================================================

func TestDisconnect_ReturnsMessage(t *testing.T) {
	bs, _ := newTestStreamer()

	msg := bs.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
	require.NotNil(t, msg)
	assert.Equal(t, protos.ConversationDisconnection_DISCONNECTION_TYPE_USER, msg.Type)
	assert.NotNil(t, msg.Time)
}

func TestDisconnect_SetsClosed(t *testing.T) {
	bs, _ := newTestStreamer()
	assert.False(t, bs.Closed)

	bs.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
	assert.True(t, bs.Closed)
}

func TestDisconnect_Idempotent(t *testing.T) {
	bs, _ := newTestStreamer()

	msg1 := bs.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
	msg2 := bs.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)

	assert.NotNil(t, msg1)
	assert.Nil(t, msg2, "Second call should return nil")
}

func TestDisconnect_ConcurrentCalls(t *testing.T) {
	bs, _ := newTestStreamer()
	var wg sync.WaitGroup
	results := make(chan *protos.ConversationDisconnection, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- bs.Disconnect(protos.ConversationDisconnection_DISCONNECTION_TYPE_USER)
		}()
	}

	wg.Wait()
	close(results)

	count := 0
	for msg := range results {
		if msg != nil {
			count++
		}
	}
	assert.Equal(t, 1, count, "Only one non-nil message regardless of concurrent calls")
}

// ============================================================================
// Integration: BufferAndSendInput + ClearInputBuffer
// ============================================================================

func TestBufferAndSendInput_ClearAndReaccumulate(t *testing.T) {
	bs, _ := newTestStreamer()

	// Buffer 300 bytes (below 480 threshold)
	bs.BufferAndSendInput(make([]byte, 300))

	// Clear
	bs.ClearInputBuffer()

	// Buffer 480 bytes → should flush (starts from 0, not 300)
	data := make([]byte, 480)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bs.BufferAndSendInput(data)

	select {
	case msg := <-bs.InputCh:
		audio := msg.(*protos.ConversationUserMessage).GetAudio()
		assert.Equal(t, 480, len(audio))
		assert.Equal(t, data, audio)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected flushed audio after clear and reaccumulate")
	}
}

// ============================================================================
// Integration: BufferAndSendOutput + ClearOutputBuffer
// ============================================================================

func TestBufferAndSendOutput_ClearAndReaccumulate(t *testing.T) {
	bs, _ := newTestStreamer()

	// Buffer 300 bytes (below threshold)
	bs.BufferAndSendOutput(make([]byte, 300))

	// Clear — resets buffer, drains channel, signals flush
	bs.ClearOutputBuffer()

	// Drain FlushAudioCh
	select {
	case <-bs.FlushAudioCh:
	default:
	}

	// Now send exactly the threshold again — should produce frames from scratch
	bs.BufferAndSendOutput(make([]byte, 480))

	framesReceived := 0
	for {
		select {
		case <-bs.OutputCh:
			framesReceived++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, 3, framesReceived, "Should produce 3 frames from fresh data after clear")
}

// ============================================================================
// Concurrent BufferAndSendInput stress test
// ============================================================================

func TestBufferAndSendInput_ConcurrentWrites(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	bs := NewBaseStreamer(logger,
		WithInputChannelSize(1000),
		WithOutputChannelSize(10),
		WithInputBufferThreshold(100),
	)

	var wg sync.WaitGroup
	writers := 10
	chunksPerWriter := 50

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < chunksPerWriter; j++ {
				bs.BufferAndSendInput(make([]byte, 20))
			}
		}()
	}

	wg.Wait()

	// Total bytes written: 10 * 50 * 20 = 10000
	// Each flush is ≥100 bytes, so expect several messages.
	totalAudioBytes := 0
	for {
		select {
		case msg := <-bs.InputCh:
			audio := msg.(*protos.ConversationUserMessage).GetAudio()
			totalAudioBytes += len(audio)
		default:
			goto done
		}
	}
done:
	// Some bytes may remain in the buffer (below threshold).
	// Total flushed + remaining should equal 10000.
	var remaining int
	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		remaining = buf.Len()
	})
	assert.Equal(t, 10000, totalAudioBytes+remaining,
		"All bytes should be accounted for (flushed + buffered remainder)")
}

// ============================================================================
// Concurrent BufferAndSendOutput stress test
// ============================================================================

func TestBufferAndSendOutput_ConcurrentWrites(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	bs := NewBaseStreamer(logger,
		WithInputChannelSize(10),
		WithOutputChannelSize(1000),
		WithInputBufferThreshold(100),
		WithOutputBufferThreshold(100),
		WithOutputFrameSize(50),
	)

	var wg sync.WaitGroup
	writers := 10
	chunksPerWriter := 50

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < chunksPerWriter; j++ {
				bs.BufferAndSendOutput(make([]byte, 20))
			}
		}()
	}

	wg.Wait()

	totalFrameBytes := 0
	for {
		select {
		case msg := <-bs.OutputCh:
			audio := msg.(*protos.ConversationAssistantMessage).GetAudio()
			assert.Equal(t, 50, len(audio), "Each frame must be exactly OutputFrameSize")
			totalFrameBytes += len(audio)
		default:
			goto done
		}
	}
done:
	var remaining int
	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		remaining = buf.Len()
	})
	assert.Equal(t, 10000, totalFrameBytes+remaining,
		"All bytes should be accounted for (flushed frames + buffered remainder)")
}

// ============================================================================
// Edge: zero thresholds
// ============================================================================

func TestBufferAndSendInput_ZeroThreshold_FlushesImmediately(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	bs := NewBaseStreamer(logger,
		WithInputChannelSize(10),
		WithOutputChannelSize(10),
		WithInputBufferThreshold(0),
	)

	// Any data should flush immediately since 0 threshold
	bs.BufferAndSendInput([]byte{1})

	select {
	case msg := <-bs.InputCh:
		audio := msg.(*protos.ConversationUserMessage).GetAudio()
		assert.Equal(t, []byte{1}, audio)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Zero threshold should flush immediately")
	}
}

func TestBufferAndSendOutput_ZeroThreshold_FlushesImmediately(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	bs := NewBaseStreamer(logger,
		WithInputChannelSize(10),
		WithOutputChannelSize(10),
		WithOutputBufferThreshold(0),
		WithOutputFrameSize(10),
	)

	// Send exactly one frame worth
	bs.BufferAndSendOutput(make([]byte, 10))

	select {
	case msg := <-bs.OutputCh:
		audio := msg.(*protos.ConversationAssistantMessage).GetAudio()
		assert.Equal(t, 10, len(audio))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Zero threshold should flush immediately")
	}
}

// ============================================================================
// BytesPerMs
// ============================================================================

func TestBytesPerMs(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *protos.AudioConfig
		expected int
	}{
		{
			name:     "nil config",
			cfg:      nil,
			expected: 0,
		},
		{
			name: "mulaw 8kHz mono",
			cfg: &protos.AudioConfig{
				SampleRate: 8000, AudioFormat: protos.AudioConfig_MuLaw8, Channels: 1,
			},
			expected: 8, // 8000 * 1 * 1 / 1000
		},
		{
			name: "linear16 8kHz mono",
			cfg: &protos.AudioConfig{
				SampleRate: 8000, AudioFormat: protos.AudioConfig_LINEAR16, Channels: 1,
			},
			expected: 16, // 8000 * 2 * 1 / 1000
		},
		{
			name: "linear16 16kHz mono",
			cfg: &protos.AudioConfig{
				SampleRate: 16000, AudioFormat: protos.AudioConfig_LINEAR16, Channels: 1,
			},
			expected: 32, // 16000 * 2 * 1 / 1000
		},
		{
			name: "linear16 48kHz mono",
			cfg: &protos.AudioConfig{
				SampleRate: 48000, AudioFormat: protos.AudioConfig_LINEAR16, Channels: 1,
			},
			expected: 96, // 48000 * 2 * 1 / 1000
		},
		{
			name: "linear16 48kHz stereo",
			cfg: &protos.AudioConfig{
				SampleRate: 48000, AudioFormat: protos.AudioConfig_LINEAR16, Channels: 2,
			},
			expected: 192, // 48000 * 2 * 2 / 1000
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, BytesPerMs(tt.cfg))
		})
	}
}

// ============================================================================
// Config accessors
// ============================================================================

func TestConfigAccessors(t *testing.T) {
	bs, _ := newTestStreamer()

	assert.Equal(t, 480, bs.InputBufferThreshold())
	assert.Equal(t, 160, bs.OutputFrameSize())
	assert.Equal(t, 480, bs.OutputBufferThreshold())
}

// ============================================================================
// Audio config integration: derived thresholds for all common formats
// ============================================================================

func TestDerivedThresholds_AllFormats(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()

	tests := []struct {
		name                 string
		cfg                  *protos.AudioConfig
		expectedInputThresh  int
		expectedOutputFrame  int
		expectedOutputThresh int
	}{
		{
			name: "mulaw 8kHz (Twilio)",
			cfg: &protos.AudioConfig{
				SampleRate: 8000, AudioFormat: protos.AudioConfig_MuLaw8, Channels: 1,
			},
			expectedInputThresh:  8 * 80, // 640
			expectedOutputFrame:  8 * 20, // 160
			expectedOutputThresh: 8 * 20, // 160
		},
		{
			name: "linear16 8kHz (Exotel)",
			cfg: &protos.AudioConfig{
				SampleRate: 8000, AudioFormat: protos.AudioConfig_LINEAR16, Channels: 1,
			},
			expectedInputThresh:  16 * 80, // 1280
			expectedOutputFrame:  16 * 20, // 320
			expectedOutputThresh: 16 * 20, // 320
		},
		{
			name: "linear16 16kHz (Vonage/Rapida)",
			cfg: &protos.AudioConfig{
				SampleRate: 16000, AudioFormat: protos.AudioConfig_LINEAR16, Channels: 1,
			},
			expectedInputThresh:  32 * 80, // 2560
			expectedOutputFrame:  32 * 20, // 640
			expectedOutputThresh: 32 * 20, // 640
		},
		{
			name: "linear16 48kHz (WebRTC)",
			cfg: &protos.AudioConfig{
				SampleRate: 48000, AudioFormat: protos.AudioConfig_LINEAR16, Channels: 1,
			},
			expectedInputThresh:  96 * 80, // 7680
			expectedOutputFrame:  96 * 20, // 1920
			expectedOutputThresh: 96 * 20, // 1920
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bs := NewBaseStreamer(logger,
				WithInputAudioConfig(tt.cfg),
				WithOutputAudioConfig(tt.cfg),
			)
			assert.Equal(t, tt.expectedInputThresh, bs.InputBufferThreshold(), "input threshold")
			assert.Equal(t, tt.expectedOutputFrame, bs.OutputFrameSize(), "output frame size")
			assert.Equal(t, tt.expectedOutputThresh, bs.OutputBufferThreshold(), "output threshold")
		})
	}
}

// ============================================================================
// Buffer pre-allocation
// ============================================================================

func TestNewBaseStreamer_BufferPreAllocation(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()

	bs := NewBaseStreamer(logger,
		WithInputBufferThreshold(480),
		WithOutputBufferThreshold(160),
		WithOutputFrameSize(160),
	)

	// Input buffer should be pre-allocated to 2× threshold.
	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		assert.GreaterOrEqual(t, buf.Cap(), 480*2, "input buffer should be pre-allocated")
	})

	// Output buffer should be pre-allocated to threshold + frame size.
	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		assert.GreaterOrEqual(t, buf.Cap(), 160+160, "output buffer should be pre-allocated")
	})
}

func TestNewBaseStreamer_BufferPreAllocation_Fallback(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()

	// No thresholds set → should fall back to 4096.
	bs := NewBaseStreamer(logger)

	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		assert.GreaterOrEqual(t, buf.Cap(), 4096, "input buffer should have fallback capacity")
	})
	bs.WithOutputBuffer(func(buf *bytes.Buffer) {
		assert.GreaterOrEqual(t, buf.Cap(), 4096, "output buffer should have fallback capacity")
	})
}

// ============================================================================
// Frame pool
// ============================================================================

func TestGetFrame_ReturnsCorrectSize(t *testing.T) {
	f := getFrame(160)
	assert.Equal(t, 160, len(f))
	assert.GreaterOrEqual(t, cap(f), 160)
	putFrame(f)
}

func TestGetFrame_PoolReuse(t *testing.T) {
	// Get and return a frame, then get again — should reuse.
	f1 := getFrame(160)
	f1[0] = 0xAA // mark it
	ptr1 := &f1[0]
	putFrame(f1)

	f2 := getFrame(160)
	// Pool reuse is best-effort; we can't guarantee same pointer,
	// but the slice should be correctly sized.
	assert.Equal(t, 160, len(f2))
	_ = ptr1 // used for debugging; pool reuse is non-deterministic
	putFrame(f2)
}

func TestGetFrame_UndersizedPooledSlice(t *testing.T) {
	// Put a small slice, then request a larger one.
	small := make([]byte, 10)
	putFrame(small)

	big := getFrame(160)
	assert.Equal(t, 160, len(big))
	assert.GreaterOrEqual(t, cap(big), 160)
	putFrame(big)
}

// ============================================================================
// BufferAndSendInput — buffer swap behaviour
// ============================================================================

func TestBufferAndSendInput_BufferSwap(t *testing.T) {
	bs, _ := newTestStreamer()

	// Fill to threshold (480 bytes).
	data := make([]byte, 480)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bs.BufferAndSendInput(data)

	// The message should be on InputCh.
	select {
	case msg := <-bs.InputCh:
		audio := msg.(*protos.ConversationUserMessage).GetAudio()
		assert.Equal(t, 480, len(audio))
		// Verify data integrity — the swap shouldn't corrupt content.
		for i := 0; i < 480; i++ {
			assert.Equal(t, byte(i%256), audio[i], "byte mismatch at index %d", i)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected message on InputCh")
	}

	// After swap, the internal buffer should be empty but pre-allocated.
	bs.WithInputBuffer(func(buf *bytes.Buffer) {
		assert.Equal(t, 0, buf.Len(), "buffer should be empty after swap")
		assert.GreaterOrEqual(t, buf.Cap(), 480, "buffer should be pre-allocated after swap")
	})
}

// ============================================================================
// BufferAndSendOutput — single-lock flush + pooled frames
// ============================================================================

func TestBufferAndSendOutput_SingleLockFlush(t *testing.T) {
	bs, _ := newTestStreamer()

	// Send 5 frames worth of data (5 × 160 = 800 bytes) in one call.
	// With single-lock flush, all 5 should be extracted under one lock.
	data := make([]byte, 800)
	for i := range data {
		data[i] = byte(i % 256)
	}
	bs.BufferAndSendOutput(data)

	// Drain all frames and verify sizes.
	var frames [][]byte
	for i := 0; i < 5; i++ {
		select {
		case msg := <-bs.OutputCh:
			audio := msg.(*protos.ConversationAssistantMessage).GetAudio()
			assert.Equal(t, 160, len(audio))
			frames = append(frames, audio)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("expected 5 frames, got %d", i)
		}
	}
	assert.Equal(t, 5, len(frames))

	// Channel should be empty now.
	select {
	case <-bs.OutputCh:
		t.Fatal("OutputCh should be empty")
	default:
	}
}

// ============================================================================
// Benchmarks — measure allocation improvements
// ============================================================================

func BenchmarkBufferAndSendOutput(b *testing.B) {
	logger, _ := commons.NewApplicationLogger()
	// Simulate WebRTC: 48kHz linear16 → 1920-byte frames.
	bs := NewBaseStreamer(logger,
		WithOutputFrameSize(1920),
		WithOutputBufferThreshold(1920),
		WithOutputChannelSize(50000),
	)

	// Pre-fill audio data: one 20ms frame per call.
	audio := make([]byte, 1920)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bs.BufferAndSendOutput(audio)
		// Drain inside loop to prevent channel overflow and WARN log noise.
		for len(bs.OutputCh) > 0 {
			<-bs.OutputCh
		}
	}
}

func BenchmarkBufferAndSendInput(b *testing.B) {
	logger, _ := commons.NewApplicationLogger()
	// Simulate µ-law 8kHz: 480-byte threshold (60ms).
	bs := NewBaseStreamer(logger,
		WithInputBufferThreshold(480),
		WithInputChannelSize(50000),
	)

	// Send 160-byte chunks (20ms of µ-law 8kHz); every 3rd call triggers flush.
	audio := make([]byte, 160)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bs.BufferAndSendInput(audio)
		// Drain inside loop to prevent channel overflow and WARN log noise.
		for len(bs.InputCh) > 0 {
			<-bs.InputCh
		}
	}
}

func BenchmarkBufferAndSendOutput_MultiFrame(b *testing.B) {
	logger, _ := commons.NewApplicationLogger()
	// Simulate telephony: 160-byte frames, but TTS sends 4800 bytes at once (30 frames).
	bs := NewBaseStreamer(logger,
		WithOutputFrameSize(160),
		WithOutputBufferThreshold(160),
		WithOutputChannelSize(50000),
	)

	// Large TTS chunk → many frames extracted under single lock.
	audio := make([]byte, 4800)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bs.BufferAndSendOutput(audio)
		// Drain to prevent channel full.
		for len(bs.OutputCh) > 0 {
			<-bs.OutputCh
		}
	}
}
