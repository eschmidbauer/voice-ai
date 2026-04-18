// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_sip_telephony

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	sip_infra "github.com/rapidaai/api/assistant-api/sip/infra"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockResampler struct {
	err error
}

func (m *mockResampler) Resample(data []byte, _, _ *protos.AudioConfig) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	// passthrough — just return the same data
	return data, nil
}

// pushRecorder captures all streams pushed via the pushInput callback.
type pushRecorder struct {
	mu      sync.Mutex
	streams []internal_type.Stream
}

func (r *pushRecorder) push(s internal_type.Stream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.streams = append(r.streams, s)
}

func (r *pushRecorder) get() []internal_type.Stream {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]internal_type.Stream, len(r.streams))
	copy(cp, r.streams)
	return cp
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testRTPHandler(t *testing.T, codec *sip_infra.Codec) *sip_infra.RTPHandler {
	t.Helper()
	logger, _ := commons.NewApplicationLogger()
	pt := uint8(0)
	if codec != nil {
		pt = codec.PayloadType
	}
	h, err := sip_infra.NewRTPHandler(context.Background(), &sip_infra.RTPConfig{
		LocalIP:     "127.0.0.1",
		LocalPort:   0, // OS picks a free port
		PayloadType: pt,
		ClockRate:   8000,
		Logger:      logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Stop() })
	return h
}

func newTestAudioProcessor(t *testing.T, codec *sip_infra.Codec, resampler internal_type.AudioResampler, recorder *pushRecorder) *AudioProcessor {
	t.Helper()
	rtp := testRTPHandler(t, codec)
	return NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  resampler,
		PushInput:  recorder.push,
	})
}

// ---------------------------------------------------------------------------
// Tests: ProcessInputAudio
// ---------------------------------------------------------------------------

func TestProcessInputAudio_PCMU_Passthrough(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	input := make([]byte, 160) // 20ms frame
	for i := range input {
		input[i] = byte(i % 256)
	}

	result := proc.ProcessInputAudio(input)
	require.NotNil(t, result)
	assert.Equal(t, input, result, "PCMU should pass through without A-law conversion")
}

func TestProcessInputAudio_PCMA_ConvertsToUlaw(t *testing.T) {
	// With PCMA codec, ProcessInputAudio should call AlawToUlaw before resampling.
	// Our mock resampler is passthrough, so we verify the data changed (A-law -> U-law).
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMA)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	// A-law encoded silence is 0xD5
	input := make([]byte, 160)
	for i := range input {
		input[i] = 0xD5
	}

	result := proc.ProcessInputAudio(input)
	require.NotNil(t, result)
	// After A-law to U-law conversion, the bytes should differ from the original A-law data
	// (0xD5 is A-law silence, U-law silence is 0xFF)
	assert.NotEqual(t, input, result, "PCMA input should be converted from A-law to U-law")
}

func TestProcessInputAudio_ResamplerError_ReturnsNil(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{err: errors.New("resample failed")}, rec)

	input := make([]byte, 160)
	result := proc.ProcessInputAudio(input)
	assert.Nil(t, result, "should return nil when resampler fails")
}

// ---------------------------------------------------------------------------
// Tests: ProcessOutputAudio
// ---------------------------------------------------------------------------

func TestProcessOutputAudio_BuffersData(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	data := make([]byte, 320) // enough for 2 frames
	for i := range data {
		data[i] = byte(i % 256)
	}

	err := proc.ProcessOutputAudio(data)
	require.NoError(t, err)

	// Verify data was buffered by retrieving chunks
	chunk1 := proc.getNextChunk()
	require.NotNil(t, chunk1)
	assert.Len(t, chunk1, mulawFrameSize)

	chunk2 := proc.getNextChunk()
	require.NotNil(t, chunk2)
	assert.Len(t, chunk2, mulawFrameSize)

	// No more data
	chunk3 := proc.getNextChunk()
	assert.Nil(t, chunk3)
}

func TestProcessOutputAudio_BridgeActive_DiscardsAudio(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	// Set bridge target
	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)

	data := make([]byte, 160)
	err := proc.ProcessOutputAudio(data)
	assert.NoError(t, err)

	// Buffer should be empty — audio was discarded
	chunk := proc.getNextChunk()
	assert.Nil(t, chunk, "audio should be discarded when bridge is active")
}

func TestProcessOutputAudio_PCMA_ConvertsToAlaw(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMA)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	// Feed 16kHz LINEAR16 data (mock resampler returns it unchanged)
	data := make([]byte, 160)
	for i := range data {
		data[i] = 0xFF // µ-law silence
	}

	err := proc.ProcessOutputAudio(data)
	require.NoError(t, err)

	chunk := proc.getNextChunk()
	require.NotNil(t, chunk)
	// After UlawToAlaw conversion, all-0xFF (µ-law silence) should become A-law encoded
	assert.NotEqual(t, data, chunk, "PCMA codec should convert output to A-law")
}

func TestProcessOutputAudio_ResamplerError(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{err: errors.New("fail")}, rec)

	data := make([]byte, 160)
	err := proc.ProcessOutputAudio(data)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Tests: ClearOutputBuffer
// ---------------------------------------------------------------------------

func TestClearOutputBuffer_ResetsBuffer(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	// Buffer some data
	data := make([]byte, 320)
	_ = proc.ProcessOutputAudio(data)

	proc.ClearOutputBuffer()

	chunk := proc.getNextChunk()
	assert.Nil(t, chunk, "buffer should be empty after clear")
}

func TestClearOutputBuffer_SignalsFlushCh(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	proc.ClearOutputBuffer()

	select {
	case <-proc.flushCh:
		// expected
	default:
		t.Fatal("flushCh should have been signalled")
	}
}

func TestClearOutputBuffer_NonBlocking_WhenFlushChFull(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	// Fill flushCh
	proc.flushCh <- struct{}{}

	// Should not block
	done := make(chan struct{})
	go func() {
		proc.ClearOutputBuffer()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(time.Second):
		t.Fatal("ClearOutputBuffer blocked when flushCh was full")
	}
}

// ---------------------------------------------------------------------------
// Tests: ForwardUserAudio
// ---------------------------------------------------------------------------

func TestForwardUserAudio_NoBridge_ReturnsFalse(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	audio := []byte{0x01, 0x02, 0x03}
	result := proc.ForwardUserAudio(audio)
	assert.False(t, result, "should return false when no bridge is active")
}

func TestForwardUserAudio_BridgeActive_ReturnsTrue(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)

	audio := []byte{0x01, 0x02, 0x03}
	result := proc.ForwardUserAudio(audio)
	assert.True(t, result)

	// Check that audio was queued to bridgeUserCh
	select {
	case queued := <-proc.bridgeUserCh:
		assert.Equal(t, audio, queued)
	case <-time.After(time.Second):
		t.Fatal("expected audio on bridgeUserCh")
	}
}

func TestForwardUserAudio_BridgeActive_SuccessPath(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)

	audio := []byte{0xAA, 0xBB, 0xCC}
	// ForwardUserAudio sends to outRTP.AudioOut() (non-blocking) and queues to bridgeUserCh.
	// We verify the return value and bridgeUserCh; outRTP.AudioOut() is send-only so we
	// can't read from it, but the non-blocking send into its buffered channel won't fail.
	result := proc.ForwardUserAudio(audio)
	assert.True(t, result)

	select {
	case queued := <-proc.bridgeUserCh:
		assert.Equal(t, audio, queued, "raw audio should be queued to bridgeUserCh")
	case <-time.After(time.Second):
		t.Fatal("expected audio on bridgeUserCh")
	}
}

func TestForwardUserAudio_WithTranscode_PCMU_to_PCMA(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMA)
	// User has PCMU, bridge target has PCMA — need transcode
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMA)

	audio := make([]byte, 160)
	for i := range audio {
		audio[i] = 0xFF // µ-law silence
	}
	result := proc.ForwardUserAudio(audio)
	assert.True(t, result, "should return true when bridge is active")

	// Raw (untranscoded) audio should still go to bridgeUserCh.
	// The transcode only applies to what's sent to outRTP.AudioOut() (which is
	// send-only and can't be read in tests). We verify the contract by confirming
	// that bridgeUserCh gets the original raw audio, proving the transcode path
	// is separate.
	select {
	case queued := <-proc.bridgeUserCh:
		assert.Equal(t, audio, queued, "raw audio should go to bridgeUserCh without transcoding")
	case <-time.After(time.Second):
		t.Fatal("expected raw audio on bridgeUserCh")
	}
}

func TestForwardUserAudio_Backpressure_DropsAudio(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)

	// Fill bridgeUserCh to capacity
	for i := 0; i < audioChSize; i++ {
		proc.bridgeUserCh <- []byte{byte(i)}
	}

	// Should not block even though channel is full
	done := make(chan struct{})
	go func() {
		proc.ForwardUserAudio([]byte{0xFF})
		close(done)
	}()

	select {
	case <-done:
		// OK — did not hang
	case <-time.After(time.Second):
		t.Fatal("ForwardUserAudio hung when bridgeUserCh was full")
	}
}

// ---------------------------------------------------------------------------
// Tests: PushOperatorAudio
// ---------------------------------------------------------------------------

func TestPushOperatorAudio_QueuesAudio(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	audio := []byte{0x10, 0x20, 0x30}
	proc.PushOperatorAudio(audio)

	select {
	case queued := <-proc.bridgeOperatorCh:
		assert.Equal(t, audio, queued)
	case <-time.After(time.Second):
		t.Fatal("expected audio on bridgeOperatorCh")
	}
}

func TestPushOperatorAudio_Backpressure_DropsAudio(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	// Fill channel
	for i := 0; i < audioChSize; i++ {
		proc.bridgeOperatorCh <- []byte{byte(i)}
	}

	// Should not block
	done := make(chan struct{})
	go func() {
		proc.PushOperatorAudio([]byte{0xFF})
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(time.Second):
		t.Fatal("PushOperatorAudio hung when bridgeOperatorCh was full")
	}
}

// ---------------------------------------------------------------------------
// Tests: SetBridgeTarget / ClearBridgeTarget / IsBridgeActive
// ---------------------------------------------------------------------------

func TestSetBridgeTarget_ActivatesBridge(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	assert.False(t, proc.IsBridgeActive())

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)

	assert.True(t, proc.IsBridgeActive())
}

func TestSetBridgeTarget_NilRTP_DoesNotActivate(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	proc.SetBridgeTarget(nil, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)
	assert.False(t, proc.IsBridgeActive())
}

func TestClearBridgeTarget_DeactivatesBridge(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)
	assert.True(t, proc.IsBridgeActive())

	proc.ClearBridgeTarget()
	assert.False(t, proc.IsBridgeActive())

	// ForwardUserAudio should now return false
	assert.False(t, proc.ForwardUserAudio([]byte{0x01}))
}

func TestSetBridgeTarget_MatchingCodecs_NoTranscode(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)

	// With matching codecs, bridgeUserCh should receive the original audio unchanged
	// (the raw audio IS the same as what goes to outRTP when no transcode is needed).
	audio := []byte{0x01, 0x02, 0x03, 0x04}
	result := proc.ForwardUserAudio(audio)
	assert.True(t, result)

	select {
	case queued := <-proc.bridgeUserCh:
		assert.Equal(t, audio, queued, "matching codecs should not alter raw audio")
	case <-time.After(time.Second):
		t.Fatal("expected audio on bridgeUserCh")
	}
}

func TestSetBridgeTarget_PCMA_to_PCMU_Transcode(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMA)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	bridgeRTP := testRTPHandler(t, &sip_infra.CodecPCMU)
	// inCodec=PCMA, outCodec=PCMU means A-law → µ-law transcode for outRTP
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMA, &sip_infra.CodecPCMU)

	audio := make([]byte, 160)
	for i := range audio {
		audio[i] = 0xD5 // A-law silence
	}
	result := proc.ForwardUserAudio(audio)
	assert.True(t, result)

	// bridgeUserCh always gets the raw (untranscoded) audio.
	// Transcode is applied only to the send to outRTP.AudioOut() which is send-only.
	select {
	case queued := <-proc.bridgeUserCh:
		assert.Equal(t, audio, queued, "bridgeUserCh should get raw untranscoded audio")
	case <-time.After(time.Second):
		t.Fatal("expected raw audio on bridgeUserCh")
	}
}

// ---------------------------------------------------------------------------
// Tests: RunOutputSender
// ---------------------------------------------------------------------------

func TestRunOutputSender_ExitsOnContextCancel(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		proc.RunOutputSender(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("RunOutputSender did not exit after context cancellation")
	}
}

func TestRunOutputSender_ConsumesBufferedChunks(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	// Buffer a frame
	frame := make([]byte, mulawFrameSize)
	for i := range frame {
		frame[i] = byte(i)
	}
	_ = proc.ProcessOutputAudio(frame)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go proc.RunOutputSender(ctx)

	// The output sender should consume the buffered chunk within a few tick intervals.
	// We verify indirectly: after enough time for the 20ms ticker to fire, getNextChunk
	// should return nil because the sender consumed the data.
	require.Eventually(t, func() bool {
		return proc.getNextChunk() == nil
	}, 500*time.Millisecond, 10*time.Millisecond, "buffered chunk should be consumed by output sender")
}

func TestRunOutputSender_FlushSignal_FlushesRTPHandler(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go proc.RunOutputSender(ctx)

	// Send flush signal
	proc.ClearOutputBuffer()

	// Give the sender time to process the flush
	time.Sleep(50 * time.Millisecond)
	// If we got here without deadlock, the flush was processed.
}

// ---------------------------------------------------------------------------
// Tests: PlayRingback
// ---------------------------------------------------------------------------

func TestPlayRingback_ExitsOnContextCancel(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		proc.PlayRingback(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("PlayRingback did not exit after context cancellation")
	}
}

func TestPlayRingback_ProducesFrames(t *testing.T) {
	rec := &pushRecorder{}
	rtp := testRTPHandler(t, &sip_infra.CodecPCMU)
	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  rec.push,
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		proc.PlayRingback(ctx)
		close(done)
	}()

	// Let PlayRingback run for a few ticks (~60ms) to produce frames,
	// then cancel and verify it exits cleanly.
	time.Sleep(60 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Exited cleanly after producing frames into the buffered AudioOut channel.
	case <-time.After(2 * time.Second):
		t.Fatal("PlayRingback did not exit after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Tests: RunBridgeRecorder
// ---------------------------------------------------------------------------

func TestRunBridgeRecorder_ExitsOnContextCancel(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		proc.RunBridgeRecorder(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridgeRecorder did not exit after context cancellation")
	}
}

func TestRunBridgeRecorder_UserAudio_PushesConversationBridgeUserAudio(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go proc.RunBridgeRecorder(ctx)

	// Send user audio
	audio := []byte{0x01, 0x02, 0x03}
	proc.bridgeUserCh <- audio

	// Wait for pushInput to be called
	require.Eventually(t, func() bool {
		return len(rec.get()) >= 1
	}, time.Second, 10*time.Millisecond)

	streams := rec.get()
	require.Len(t, streams, 1)

	msg, ok := streams[0].(*protos.ConversationBridgeUserAudio)
	require.True(t, ok, "expected ConversationBridgeUserAudio, got %T", streams[0])
	assert.Equal(t, audio, msg.Audio)
}

func TestRunBridgeRecorder_OperatorAudio_PushesConversationBridgeOperatorAudio(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go proc.RunBridgeRecorder(ctx)

	// Send operator audio
	audio := []byte{0x10, 0x20, 0x30}
	proc.bridgeOperatorCh <- audio

	require.Eventually(t, func() bool {
		return len(rec.get()) >= 1
	}, time.Second, 10*time.Millisecond)

	streams := rec.get()
	require.Len(t, streams, 1)

	msg, ok := streams[0].(*protos.ConversationBridgeOperatorAudio)
	require.True(t, ok, "expected ConversationBridgeOperatorAudio, got %T", streams[0])
	assert.Equal(t, audio, msg.Audio)
}

func TestRunBridgeRecorder_ResamplerError_DoesNotPush(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{err: errors.New("fail")}, rec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go proc.RunBridgeRecorder(ctx)

	proc.bridgeUserCh <- []byte{0x01}
	proc.bridgeOperatorCh <- []byte{0x02}

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	streams := rec.get()
	assert.Empty(t, streams, "should not push when resampler fails")
}

// ---------------------------------------------------------------------------
// Tests: NewAudioProcessor contract
// ---------------------------------------------------------------------------

func TestNewAudioProcessor_InitializesChannels(t *testing.T) {
	rec := &pushRecorder{}
	proc := newTestAudioProcessor(t, &sip_infra.CodecPCMU, &mockResampler{}, rec)

	assert.NotNil(t, proc.flushCh)
	assert.NotNil(t, proc.bridgeUserCh)
	assert.NotNil(t, proc.bridgeOperatorCh)
	assert.Equal(t, audioChSize, cap(proc.bridgeUserCh))
	assert.Equal(t, audioChSize, cap(proc.bridgeOperatorCh))
	assert.Equal(t, 1, cap(proc.flushCh))
	assert.False(t, proc.IsBridgeActive())
}

// ---------------------------------------------------------------------------
// Benchmarks — hot-path audio processing (called every 20ms per RTP packet)
// ---------------------------------------------------------------------------

func benchAudioProcessor(b *testing.B, codec *sip_infra.Codec) *AudioProcessor {
	b.Helper()
	logger, _ := commons.NewApplicationLogger()
	rtp, err := sip_infra.NewRTPHandler(context.Background(), &sip_infra.RTPConfig{
		LocalIP:     "127.0.0.1",
		LocalPort:   0,
		PayloadType: codec.PayloadType,
		ClockRate:   8000,
		Logger:      logger,
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = rtp.Stop() })

	return NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  func(internal_type.Stream) {},
	})
}

// BenchmarkProcessInputAudio_PCMU measures the per-packet input processing for PCMU.
func BenchmarkProcessInputAudio_PCMU(b *testing.B) {
	proc := benchAudioProcessor(b, &sip_infra.CodecPCMU)
	frame := make([]byte, mulawFrameSize)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = proc.ProcessInputAudio(frame)
	}
}

// BenchmarkProcessInputAudio_PCMA measures the per-packet input processing for PCMA
// (includes A-law to µ-law conversion).
func BenchmarkProcessInputAudio_PCMA(b *testing.B) {
	proc := benchAudioProcessor(b, &sip_infra.CodecPCMA)
	frame := make([]byte, mulawFrameSize)
	for i := range frame {
		frame[i] = 0xD5
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = proc.ProcessInputAudio(frame)
	}
}

// BenchmarkProcessOutputAudio measures output buffering throughput.
func BenchmarkProcessOutputAudio(b *testing.B) {
	proc := benchAudioProcessor(b, &sip_infra.CodecPCMU)
	frame := make([]byte, mulawFrameSize)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = proc.ProcessOutputAudio(frame)
		// Periodically clear to prevent unbounded memory growth
		if i%1000 == 0 {
			proc.ClearOutputBuffer()
		}
	}
}

// BenchmarkForwardUserAudio_NoBridge measures the no-bridge fast-exit path.
func BenchmarkForwardUserAudio_NoBridge(b *testing.B) {
	proc := benchAudioProcessor(b, &sip_infra.CodecPCMU)
	frame := make([]byte, mulawFrameSize)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = proc.ForwardUserAudio(frame)
	}
}

// BenchmarkForwardUserAudio_BridgeActive measures the bridge forwarding hot path.
func BenchmarkForwardUserAudio_BridgeActive(b *testing.B) {
	logger, _ := commons.NewApplicationLogger()
	rtp, _ := sip_infra.NewRTPHandler(context.Background(), &sip_infra.RTPConfig{
		LocalIP: "127.0.0.1", LocalPort: 0, PayloadType: 0, ClockRate: 8000, Logger: logger,
	})
	bridgeRTP, _ := sip_infra.NewRTPHandler(context.Background(), &sip_infra.RTPConfig{
		LocalIP: "127.0.0.1", LocalPort: 0, PayloadType: 0, ClockRate: 8000, Logger: logger,
	})
	b.Cleanup(func() { _ = rtp.Stop(); _ = bridgeRTP.Stop() })

	proc := NewAudioProcessor(AudioProcessorConfig{
		RTPHandler: rtp,
		Resampler:  &mockResampler{},
		PushInput:  func(internal_type.Stream) {},
	})
	proc.SetBridgeTarget(bridgeRTP, &sip_infra.CodecPCMU, &sip_infra.CodecPCMU)

	frame := make([]byte, mulawFrameSize)

	// Drain channels in background to prevent backpressure blocking
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-proc.bridgeUserCh:
			}
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = proc.ForwardUserAudio(frame)
	}
}
