package adapter_internal

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	rapida_types "github.com/rapidaai/pkg/types"
	"github.com/rapidaai/protos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dispatcherTestStreamer struct {
	ctx context.Context
}

func (s *dispatcherTestStreamer) Context() context.Context { return s.ctx }
func (s *dispatcherTestStreamer) Recv() (internal_type.Stream, error) {
	return nil, io.EOF
}
func (s *dispatcherTestStreamer) Send(internal_type.Stream) error { return nil }
func (s *dispatcherTestStreamer) NotifyMode(protos.StreamMode)    {}

type executorCaptureStub struct {
	packetCh chan internal_type.Packet
	err      error
}

func (e *executorCaptureStub) Initialize(context.Context, internal_type.Communication, *protos.ConversationInitialization) error {
	return nil
}
func (e *executorCaptureStub) Name() string { return "capture" }
func (e *executorCaptureStub) Execute(_ context.Context, _ internal_type.Communication, pkt internal_type.Packet) error {
	e.packetCh <- pkt
	return e.err
}
func (e *executorCaptureStub) Close(context.Context) error { return nil }

type sttCaptureStub struct {
	packetCh chan internal_type.UserAudioPacket
	err      error
}

func (s *sttCaptureStub) Name() string { return "stt-capture" }
func (s *sttCaptureStub) Initialize() error {
	return nil
}
func (s *sttCaptureStub) Transform(_ context.Context, pkt internal_type.UserAudioPacket) error {
	if s.packetCh != nil {
		s.packetCh <- pkt
	}
	return s.err
}
func (s *sttCaptureStub) Close(context.Context) error { return nil }

type eosCaptureStub struct {
	packetCh chan internal_type.Packet
	err      error
}

func (e *eosCaptureStub) Name() string { return "eos-capture" }
func (e *eosCaptureStub) Analyze(_ context.Context, pkt internal_type.Packet) error {
	if e.packetCh != nil {
		e.packetCh <- pkt
	}
	return e.err
}
func (e *eosCaptureStub) Close() error { return nil }

type normalizerStub struct {
	onPacket     func(...internal_type.Packet) error
	normalizeCnt int64
	err          error
}

func (n *normalizerStub) Initialize(_ context.Context, onPacket func(...internal_type.Packet) error) error {
	n.onPacket = onPacket
	return nil
}

func (n *normalizerStub) Normalize(_ context.Context, packets ...internal_type.Packet) error {
	atomic.AddInt64(&n.normalizeCnt, 1)
	if n.err != nil {
		return n.err
	}
	if n.onPacket == nil || len(packets) == 0 {
		return nil
	}
	eos, ok := packets[0].(internal_type.EndOfSpeechPacket)
	if !ok {
		return nil
	}
	lang := rapida_types.LookupLanguage("en")
	if len(eos.Speechs) > 0 && eos.Speechs[0].Language != "" {
		if found := rapida_types.LookupLanguage(eos.Speechs[0].Language); found != nil {
			lang = found
		}
	}
	return n.onPacket(internal_type.NormalizedUserTextPacket{
		ContextID: eos.ContextID,
		Text:      eos.Speech,
		Language:  lang,
	})
}

func (n *normalizerStub) Close(context.Context) error { return nil }

func dispatchTestLogger(t *testing.T) commons.Logger {
	t.Helper()
	logger, err := commons.NewApplicationLogger(
		commons.Name("dispatch-test"),
		commons.Level("error"),
		commons.EnableFile(false),
	)
	require.NoError(t, err)
	return logger
}

func mustLang(t *testing.T, code string) *rapida_types.Language {
	t.Helper()
	lang := rapida_types.LookupLanguage(code)
	require.NotNil(t, lang)
	return lang
}

func TestHandleNormalizedText_EnqueuesExecuteLLMWithNormalizedPacket(t *testing.T) {
	r := &genericRequestor{
		logger:           dispatchTestLogger(t),
		streamer:         &dispatcherTestStreamer{ctx: context.Background()},
		contextID:        "ctx-1",
		interactionState: Unknown,
		outputCh:         make(chan packetEnvelope, 2),
		lowCh:            make(chan packetEnvelope, 2),
	}

	r.handleNormalizedText(context.Background(), internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-1",
		Text:      "bonjour",
		Language:  mustLang(t, "fr"),
	})

	select {
	case env := <-r.lowCh:
		save, ok := env.pkt.(internal_type.SaveMessagePacket)
		require.True(t, ok)
		assert.Equal(t, "ctx-1", save.ContextID)
		assert.Equal(t, "bonjour", save.Text)
		assert.Equal(t, "fr", save.Language)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SaveMessagePacket")
	}

	select {
	case env := <-r.outputCh:
		exec, ok := env.pkt.(internal_type.ExecuteLLMPacket)
		require.True(t, ok)
		require.NotNil(t, exec.Normalized)
		assert.Equal(t, "ctx-1", exec.ContextID)
		assert.Equal(t, "bonjour", exec.Normalized.Text)
		assert.Equal(t, "fr", exec.Normalized.Language.ISO639_1)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ExecuteLLMPacket")
	}
}

func TestHandleExecuteLLM_PrefersNormalizedPacket(t *testing.T) {
	executor := &executorCaptureStub{packetCh: make(chan internal_type.Packet, 1)}
	r := &genericRequestor{
		logger:            dispatchTestLogger(t),
		assistantExecutor: executor,
	}
	normalized := internal_type.NormalizedUserTextPacket{
		ContextID: "ctx-2",
		Text:      "hola",
		Language:  mustLang(t, "es"),
	}

	r.handleExecuteLLM(context.Background(), internal_type.ExecuteLLMPacket{
		ContextID:  "ctx-2",
		Input:      "hola",
		Language:   "es",
		Normalized: &normalized,
	})

	select {
	case pkt := <-executor.packetCh:
		got, ok := pkt.(internal_type.NormalizedUserTextPacket)
		require.True(t, ok)
		assert.Equal(t, "ctx-2", got.ContextID)
		assert.Equal(t, "hola", got.Text)
		assert.Equal(t, "es", got.Language.ISO639_1)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for executor packet")
	}
}

func TestHandleExecuteLLM_FallsBackToUserTextPacket(t *testing.T) {
	executor := &executorCaptureStub{packetCh: make(chan internal_type.Packet, 1)}
	r := &genericRequestor{
		logger:            dispatchTestLogger(t),
		assistantExecutor: executor,
	}

	r.handleExecuteLLM(context.Background(), internal_type.ExecuteLLMPacket{
		ContextID: "ctx-3",
		Input:     "hello",
		Language:  "en",
	})

	select {
	case pkt := <-executor.packetCh:
		got, ok := pkt.(internal_type.UserTextPacket)
		require.True(t, ok)
		assert.Equal(t, "ctx-3", got.ContextID)
		assert.Equal(t, "hello", got.Text)
		assert.Equal(t, "en", got.Language)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for executor packet")
	}
}

func TestHandleUserAudio_RoutesToSTTAndEOSAndQueuesPackets(t *testing.T) {
	stt := &sttCaptureStub{packetCh: make(chan internal_type.UserAudioPacket, 1)}
	eos := &eosCaptureStub{packetCh: make(chan internal_type.Packet, 1)}
	r := &genericRequestor{
		logger:                  dispatchTestLogger(t),
		speechToTextTransformer: stt,
		endOfSpeech:             eos,
		inputCh:                 make(chan packetEnvelope, 2),
		lowCh:                   make(chan packetEnvelope, 2),
	}

	audio := internal_type.UserAudioPacket{ContextID: "ctx-audio", Audio: []byte{1, 2, 3, 4}}
	r.handleUserAudio(context.Background(), audio)

	select {
	case got := <-stt.packetCh:
		assert.Equal(t, "ctx-audio", got.ContextID)
		assert.Equal(t, audio.Audio, got.Audio)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for STT transform call")
	}

	select {
	case pkt := <-eos.packetCh:
		_, ok := pkt.(internal_type.UserAudioPacket)
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EOS analyze call")
	}

	select {
	case env := <-r.lowCh:
		_, ok := env.pkt.(internal_type.RecordUserAudioPacket)
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RecordUserAudioPacket")
	}

	select {
	case env := <-r.inputCh:
		_, ok := env.pkt.(internal_type.VadAudioPacket)
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for VadAudioPacket")
	}
}

func TestHandleUserAudio_WithDenoiserQueuesDenoiseOnly(t *testing.T) {
	stt := &sttCaptureStub{packetCh: make(chan internal_type.UserAudioPacket, 1)}
	eos := &eosCaptureStub{packetCh: make(chan internal_type.Packet, 1)}
	denoiser := denoiserStub{}

	r := &genericRequestor{
		logger:                  dispatchTestLogger(t),
		speechToTextTransformer: stt,
		endOfSpeech:             eos,
		denoiser:                denoiser,
		inputCh:                 make(chan packetEnvelope, 2),
		lowCh:                   make(chan packetEnvelope, 2),
	}

	r.handleUserAudio(context.Background(), internal_type.UserAudioPacket{
		ContextID: "ctx-denoise",
		Audio:     []byte{9, 9},
	})

	select {
	case env := <-r.inputCh:
		_, ok := env.pkt.(internal_type.DenoiseAudioPacket)
		require.True(t, ok)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for DenoiseAudioPacket")
	}

	select {
	case <-stt.packetCh:
		t.Fatal("STT should not be called before denoise callback")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case <-eos.packetCh:
		t.Fatal("EOS should not be called before denoise callback")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHandleSpeechToText_NoEOS_FinalFallsBackToEndOfSpeechPacket(t *testing.T) {
	r := &genericRequestor{
		logger:  dispatchTestLogger(t),
		inputCh: make(chan packetEnvelope, 1),
	}

	r.handleSpeechToText(context.Background(), internal_type.SpeechToTextPacket{
		ContextID: "ctx-stt",
		Script:    "hello world",
		Interim:   false,
	})

	select {
	case env := <-r.inputCh:
		eos, ok := env.pkt.(internal_type.EndOfSpeechPacket)
		require.True(t, ok)
		assert.Equal(t, "hello world", eos.Speech)
		assert.Len(t, eos.Speechs, 1)
		assert.Equal(t, "hello world", eos.Speechs[0].Script)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fallback EndOfSpeechPacket")
	}
}

func TestHandleSpeechToText_NoEOS_InterimDoesNotEmitFallback(t *testing.T) {
	r := &genericRequestor{
		logger:  dispatchTestLogger(t),
		inputCh: make(chan packetEnvelope, 1),
	}

	r.handleSpeechToText(context.Background(), internal_type.SpeechToTextPacket{
		ContextID: "ctx-stt",
		Script:    "partial",
		Interim:   true,
	})

	select {
	case <-r.inputCh:
		t.Fatal("interim STT should not emit fallback EndOfSpeechPacket")
	case <-time.After(120 * time.Millisecond):
	}
}

func TestPipelineRegression_STTFinalToExecuteLLM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	executor := &executorCaptureStub{packetCh: make(chan internal_type.Packet, 1)}
	norm := &normalizerStub{}
	r := &genericRequestor{
		logger:            dispatchTestLogger(t),
		streamer:          &dispatcherTestStreamer{ctx: ctx},
		contextID:         "ctx-e2e",
		interactionState:  Unknown,
		assistantExecutor: executor,
		normalizer:        norm,
		criticalCh:        make(chan packetEnvelope, 4),
		inputCh:           make(chan packetEnvelope, 16),
		outputCh:          make(chan packetEnvelope, 8),
		lowCh:             make(chan packetEnvelope, 8),
	}
	require.NoError(t, norm.Initialize(ctx, func(pkts ...internal_type.Packet) error {
		return r.OnPacket(ctx, pkts...)
	}))

	go r.runInputDispatcher(ctx)
	go r.runOutputDispatcher(ctx)

	err := r.OnPacket(ctx, internal_type.SpeechToTextPacket{
		ContextID: "ctx-e2e",
		Script:    "hello pipeline",
		Language:  "en",
		Interim:   false,
	})
	require.NoError(t, err)

	select {
	case pkt := <-executor.packetCh:
		got, ok := pkt.(internal_type.NormalizedUserTextPacket)
		require.True(t, ok, "executor should receive normalized packet")
		assert.Equal(t, "ctx-e2e", got.ContextID)
		assert.Equal(t, "hello pipeline", got.Text)
		require.NotNil(t, got.Language)
		assert.Equal(t, "en", got.Language.ISO639_1)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for executor packet in e2e pipeline")
	}

	assert.GreaterOrEqual(t, atomic.LoadInt64(&norm.normalizeCnt), int64(1))
}

type denoiserStub struct{}

func (d denoiserStub) Denoise(context.Context, internal_type.DenoiseAudioPacket) error { return nil }
func (d denoiserStub) Close() error                                                     { return nil }
