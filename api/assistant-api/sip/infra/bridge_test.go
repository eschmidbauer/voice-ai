package sip_infra

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapidaai/pkg/commons"
)

func bridgeTestLogger() commons.Logger {
	l, _ := commons.NewApplicationLogger(commons.Level("debug"))
	return l
}

// newTestRTPHandler creates an RTPHandler with pre-made channels and no real UDP socket.
func newTestRTPHandler() *RTPHandler {
	ctx, cancel := context.WithCancel(context.Background())
	h := &RTPHandler{
		audioInChan:  make(chan []byte, 100),
		audioOutChan: make(chan []byte, 100),
		flushAudioCh: make(chan struct{}, 1),
		ctx:          ctx,
		cancel:       cancel,
	}
	h.running.Store(true)
	return h
}

func bridgeTestConfig() *Config {
	return &Config{
		Server:            "127.0.0.1",
		Port:              5060,
		Username:          "testuser",
		Password:          "testpass",
		RTPPortRangeStart: 10000,
		RTPPortRangeEnd:   10020,
	}
}

// newBridgeTestSession creates a session with an in-memory RTP handler attached.
func newBridgeTestSession(t *testing.T, direction CallDirection, codec *Codec) (*Session, *RTPHandler) {
	t.Helper()
	s, err := NewSession(context.Background(), &SessionConfig{
		Config:    bridgeTestConfig(),
		Direction: direction,
		Codec:     codec,
		Logger:    bridgeTestLogger(),
	})
	require.NoError(t, err)
	rtp := newTestRTPHandler()
	s.SetRTPHandler(rtp)
	if codec != nil {
		s.SetNegotiatedCodec(codec.Name, int(codec.ClockRate))
	}
	return s, rtp
}

func bridgeTestServer() *Server {
	return &Server{logger: bridgeTestLogger()}
}

// =============================================================================
// transcodeG711 — codec pairs
// =============================================================================

func TestTranscodeG711_SameCodecPassthrough(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()
	data := []byte{0xFF, 0x7F, 0x00, 0xAA}

	out := srv.transcodeG711(data, &CodecPCMU, &CodecPCMU)
	assert.Equal(t, data, out, "PCMU→PCMU should passthrough")

	out = srv.transcodeG711(data, &CodecPCMA, &CodecPCMA)
	assert.Equal(t, data, out, "PCMA→PCMA should passthrough")
}

func TestTranscodeG711_PCMAtoPCMU(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()
	alaw := []byte{0xD5, 0xD5, 0xD5, 0xD5}
	out := srv.transcodeG711(alaw, &CodecPCMA, &CodecPCMU)
	require.Len(t, out, len(alaw))
	assert.NotEqual(t, alaw, out, "should be transcoded")
}

func TestTranscodeG711_PCMUtoPCMA(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()
	ulaw := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	out := srv.transcodeG711(ulaw, &CodecPCMU, &CodecPCMA)
	require.Len(t, out, len(ulaw))
	assert.NotEqual(t, ulaw, out, "should be transcoded")
}

func TestTranscodeG711_Roundtrip(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()
	original := make([]byte, 160)
	for i := range original {
		original[i] = byte(i)
	}
	alaw := srv.transcodeG711(original, &CodecPCMU, &CodecPCMA)
	back := srv.transcodeG711(alaw, &CodecPCMA, &CodecPCMU)
	require.Len(t, back, len(original), "roundtrip must preserve length")
}

func TestTranscodeG711_UnknownCodecPassthrough(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()
	data := []byte{0x01, 0x02}
	g722 := &Codec{Name: "G722", PayloadType: 9, ClockRate: 16000}
	out := srv.transcodeG711(data, g722, &CodecPCMU)
	assert.Equal(t, data, out, "unknown codec pair should passthrough")
}

func TestTranscodeG711_EmptyData(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()
	out := srv.transcodeG711([]byte{}, &CodecPCMA, &CodecPCMU)
	assert.Empty(t, out)
}

// =============================================================================
// forwardBridgeAudio
// =============================================================================

func TestForwardBridgeAudio_PassthroughSameCodec(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	src := make(chan []byte, 10)
	dst := make(chan []byte, 10)
	ctx, cancel := context.WithCancel(context.Background())

	go srv.forwardBridgeAudio(ctx, src, dst, false, &CodecPCMU, &CodecPCMU)

	for i := 0; i < 5; i++ {
		src <- []byte{byte(i), byte(i + 1)}
	}
	for i := 0; i < 5; i++ {
		select {
		case frame := <-dst:
			assert.Equal(t, []byte{byte(i), byte(i + 1)}, frame)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for frame %d", i)
		}
	}
	cancel()
}

func TestForwardBridgeAudio_TranscodesWhenNeeded(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	src := make(chan []byte, 10)
	dst := make(chan []byte, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.forwardBridgeAudio(ctx, src, dst, true, &CodecPCMA, &CodecPCMU)

	alaw := []byte{0xD5, 0xD5}
	src <- alaw

	select {
	case frame := <-dst:
		assert.NotEqual(t, alaw, frame, "should be transcoded")
		assert.Len(t, frame, len(alaw))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for transcoded frame")
	}
}

func TestForwardBridgeAudio_ExitsOnContextCancel(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	src := make(chan []byte, 10)
	dst := make(chan []byte, 10)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		srv.forwardBridgeAudio(ctx, src, dst, false, &CodecPCMU, &CodecPCMU)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not exit after context cancel")
	}
}

func TestForwardBridgeAudio_ExitsOnSrcClose(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	src := make(chan []byte, 10)
	dst := make(chan []byte, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		srv.forwardBridgeAudio(ctx, src, dst, false, &CodecPCMU, &CodecPCMU)
		close(done)
	}()

	close(src)

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not exit after src close")
	}
}

func TestForwardBridgeAudio_DropsFrameWhenDstFull(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	src := make(chan []byte, 10)
	dst := make(chan []byte, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.forwardBridgeAudio(ctx, src, dst, false, &CodecPCMU, &CodecPCMU)

	// Fill dst
	src <- []byte{0x01}
	time.Sleep(10 * time.Millisecond)

	// Send more — should be dropped, not block
	for i := 0; i < 5; i++ {
		src <- []byte{byte(i + 2)}
	}

	select {
	case frame := <-dst:
		assert.Equal(t, []byte{0x01}, frame)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("forwardBridgeAudio is blocked")
	}
}

// =============================================================================
// BridgeTransfer
// =============================================================================

func TestBridgeTransfer_NilInboundRTP_EndsBothSessions(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound := newInboundTestSession(t) // no RTP handler
	outbound, _ := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	err := srv.BridgeTransfer(context.Background(), inbound, outbound)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRTPNotInitialized)
	assert.True(t, inbound.IsEnded())
	assert.True(t, outbound.IsEnded())
}

func TestBridgeTransfer_NilOutboundRTP_EndsBothSessions(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, _ := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMU)
	outbound := newInboundTestSession(t) // no RTP handler

	err := srv.BridgeTransfer(context.Background(), inbound, outbound)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRTPNotInitialized)
	assert.True(t, inbound.IsEnded())
	assert.True(t, outbound.IsEnded())
}

func TestBridgeTransfer_ContextCancellation(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, _ := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMU)
	outbound, _ := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.BridgeTransfer(ctx, inbound, outbound)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("BridgeTransfer did not exit after context cancel")
	}
	assert.True(t, inbound.IsEnded())
	assert.True(t, outbound.IsEnded())
}

func TestBridgeTransfer_InboundByeEndsBridge(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, _ := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMU)
	outbound, _ := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	done := make(chan error, 1)
	go func() {
		done <- srv.BridgeTransfer(context.Background(), inbound, outbound)
	}()

	time.Sleep(10 * time.Millisecond)
	inbound.NotifyBye()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("BridgeTransfer did not exit after inbound BYE")
	}
	assert.True(t, outbound.IsEnded())
}

func TestBridgeTransfer_OutboundByeEndsBridge(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, _ := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMU)
	outbound, _ := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	done := make(chan error, 1)
	go func() {
		done <- srv.BridgeTransfer(context.Background(), inbound, outbound)
	}()

	time.Sleep(10 * time.Millisecond)
	outbound.NotifyBye()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("BridgeTransfer did not exit after outbound BYE")
	}
	assert.True(t, inbound.IsEnded())
}

func TestBridgeTransfer_SessionEndTerminatesBridge(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, _ := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMU)
	outbound, _ := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	done := make(chan error, 1)
	go func() {
		done <- srv.BridgeTransfer(context.Background(), inbound, outbound)
	}()

	time.Sleep(10 * time.Millisecond)
	inbound.End()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("BridgeTransfer did not exit after inbound End()")
	}
	assert.True(t, outbound.IsEnded())
}

func TestBridgeTransfer_AudioForwardsBidirectionally(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, inRTP := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMU)
	outbound, outRTP := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.BridgeTransfer(ctx, inbound, outbound)
	}()

	// outbound → inbound (inbound→outbound is handled by streamer's forwardIncomingAudio)
	outRTP.audioInChan <- []byte{0x03, 0x04}
	select {
	case frame := <-inRTP.audioOutChan:
		assert.Equal(t, []byte{0x03, 0x04}, frame)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audio not forwarded outbound→inbound")
	}

	cancel()
	<-done
}

func TestBridgeTransfer_TranscodesAcrossCodecs(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, inRTP := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMA)
	outbound, outRTP := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.BridgeTransfer(ctx, inbound, outbound)
	}()

	// µ-law from outbound → A-law on inbound
	ulaw := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	outRTP.audioInChan <- ulaw
	select {
	case frame := <-inRTP.audioOutChan:
		assert.Len(t, frame, len(ulaw))
		assert.NotEqual(t, ulaw, frame, "should be transcoded PCMU→PCMA")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("transcoded audio not forwarded")
	}

	cancel()
	<-done
}

func TestBridgeTransfer_AlreadyEndedSessions(t *testing.T) {
	t.Parallel()
	srv := bridgeTestServer()

	inbound, _ := newBridgeTestSession(t, CallDirectionInbound, &CodecPCMU)
	outbound, _ := newBridgeTestSession(t, CallDirectionOutbound, &CodecPCMU)

	inbound.End()
	outbound.End()

	// RTP handler is nil after End() clears it
	err := srv.BridgeTransfer(context.Background(), inbound, outbound)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRTPNotInitialized)
}

// =============================================================================
// codecName
// =============================================================================

func TestCodecName_Nil(t *testing.T) {
	srv := bridgeTestServer()
	assert.Equal(t, "PCMU", srv.codecName(nil))
}

func TestCodecName_PCMA(t *testing.T) {
	srv := bridgeTestServer()
	assert.Equal(t, "PCMA", srv.codecName(&CodecPCMA))
}

// =============================================================================
// outboundInvite cleanup
// =============================================================================

func TestOutboundInviteCleanup_StopsRTPHandler(t *testing.T) {
	t.Parallel()
	rtp := newTestRTPHandler()
	require.True(t, rtp.running.Load())

	invite := &outboundInvite{
		rtpHandler: rtp,
	}
	invite.cleanup()
	assert.False(t, rtp.running.Load(), "RTP handler should be stopped")
}

func TestOutboundInviteCleanup_NilRTPHandler(t *testing.T) {
	t.Parallel()
	invite := &outboundInvite{
		rtpHandler: nil,
	}
	// Should not panic
	invite.cleanup()
}

func TestOutboundInviteCleanup_DoubleCleanup(t *testing.T) {
	t.Parallel()
	rtp := newTestRTPHandler()
	invite := &outboundInvite{
		rtpHandler: rtp,
	}
	invite.cleanup()
	invite.cleanup() // second call should not panic
	assert.False(t, rtp.running.Load())
}
