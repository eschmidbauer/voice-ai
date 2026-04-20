// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

// Package channel_base provides the foundational BaseStreamer that every
// concrete streamer (WebRTC, telephony WebSocket, SIP, Asterisk, …) embeds.
//
// # Core API
//
// BaseStreamer owns transport-agnostic channel and buffer management:
//
//   - InputCh / OutputCh — ordered, typed message channels (sized via options)
//   - inputAudioBuffer / outputAudioBuffer — PCM accumulation with configurable thresholds
//   - FlushAudioCh — interrupt signalling for the output writer
//   - Input / PushOutput — non-blocking type-routed sends into CriticalCh / InputCh / LowCh / OutputCh
//   - BufferAndSendInput — accumulate input PCM, flush at threshold into InputCh
//   - BufferAndSendOutput — accumulate output PCM, flush fixed-size 20 ms frames into OutputCh
//   - ClearInputBuffer / ClearOutputBuffer — drain buffers and channels (interruption)
//   - WithInputBuffer / WithOutputBuffer — synchronous buffer access under lock
//   - ResetInputBuffer / ResetOutputBuffer — quick buffer reset under lock
//   - Disconnect — idempotent disconnect signal (returns message for caller to push)
//   - Context / Recv — Streamer interface helpers consumed by the Talk loop
//
// # Configuration
//
// Use functional options (Option) to override defaults:
//
//	bs := channel_base.NewBaseStreamer(logger,
//	    channel_base.WithInputChannelSize(500),
//	    channel_base.WithOutputChannelSize(1500),
//	    channel_base.WithOutputAudioConfig(audioConfig48kHz),
//	)
//
// Default output frame duration is 20 ms. Frame size and buffer thresholds
// are automatically derived from the audio config (bytes_per_ms × duration).
// See individual With* functions for details.
//
// # Usage Patterns
//
// Channel-based (WebRTC, channel consumers):
//   - Background goroutines call BufferAndSendInput / BufferAndSendOutput
//   - Talk loop calls Recv() to read from InputCh
//   - Output writer goroutine reads from OutputCh
//
// Synchronous (telephony WebSocket):
//   - Recv() reads from the WebSocket inline
//   - Send() writes audio using WithOutputBuffer for direct buffer access
//   - ClearOutputBuffer is used on interruption
package channel_base

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	internal_audio "github.com/rapidaai/api/assistant-api/internal/audio"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ============================================================================
// Frame pool — eliminates per-frame heap allocations in the output hot path.
// ============================================================================

// framePool recycles fixed-size byte slices used by BufferAndSendOutput.
// The pool is sized to the output frame size (typically 160–1920 bytes
// depending on codec/sample-rate). Callers must return slices via putFrame
// after the downstream consumer has finished with the data.
//
// sync.Pool is safe for concurrent use and its per-P caching avoids
// cross-goroutine contention on the hot path.
var framePool = sync.Pool{
	New: func() interface{} {
		// Fallback: allocate a default-sized slice.
		// In practice getFrame(n) always creates correctly-sized slices.
		return make([]byte, 0)
	},
}

// getFrame returns a []byte of exactly n bytes from the pool.
// If the pooled slice is too small it is discarded and a fresh one allocated.
func getFrame(n int) []byte {
	if b, ok := framePool.Get().([]byte); ok && cap(b) >= n {
		return b[:n]
	}
	return make([]byte, n)
}

// putFrame returns a frame slice to the pool for reuse.
func putFrame(b []byte) {
	framePool.Put(b) //nolint:staticcheck // slice is intentionally pooled
}

// ============================================================================
// Default constants
// ============================================================================

const (
	// DefaultInputChannelSize is the default buffered channel capacity for
	// incoming messages. Suitable for most telephony transports.
	DefaultInputChannelSize = 100

	// DefaultOutputChannelSize is the default buffered channel capacity for
	// outgoing messages.
	DefaultOutputChannelSize = 500

	// DefaultFrameDurationMs is the standard output frame duration in
	// milliseconds. All streamers produce 20 ms audio frames.
	DefaultFrameDurationMs = 20

	// DefaultInputDurationMs is the default input buffer accumulation
	// duration in milliseconds before flushing to InputCh.
	// 60 ms provides ~2 Silero VAD windows (512 samples each at 16kHz),
	// improving detection stability with minimal added latency.
	DefaultInputDurationMs = 80
)

// ============================================================================
// Option — functional options for BaseStreamer configuration
// ============================================================================

// streamerConfig holds resolved configuration after applying all options.
// Unexported to enforce the Option API.
type streamerConfig struct {
	inputChannelSize  int
	outputChannelSize int

	// Input threshold in bytes. If set explicitly, used as-is.
	// Otherwise derived from inputAudioConfig × DefaultInputDurationMs.
	inputBufferThreshold int

	// Output threshold and frame size in bytes. If set explicitly, used as-is.
	// Otherwise derived from outputAudioConfig × DefaultFrameDurationMs.
	outputBufferThreshold int
	outputFrameSize       int

	// Audio configs used to auto-derive byte thresholds when explicit
	// values are not provided.
	inputAudioConfig  *protos.AudioConfig
	outputAudioConfig *protos.AudioConfig

	// Flags to know whether caller set explicit byte thresholds.
	inputThresholdSet  bool
	outputThresholdSet bool
	outputFrameSet     bool
}

// Option configures a BaseStreamer. Pass one or more options to NewBaseStreamer.
type Option func(*streamerConfig)

// WithInputChannelSize sets the buffered channel capacity for InputCh.
// Default: DefaultInputChannelSize (100).
func WithInputChannelSize(n int) Option {
	return func(c *streamerConfig) { c.inputChannelSize = n }
}

// WithOutputChannelSize sets the buffered channel capacity for OutputCh.
// Default: DefaultOutputChannelSize (500).
func WithOutputChannelSize(n int) Option {
	return func(c *streamerConfig) { c.outputChannelSize = n }
}

// WithInputBufferThreshold sets the exact byte count that triggers flushing
// the input audio buffer into InputCh. This overrides any value derived from
// the input audio config.
func WithInputBufferThreshold(n int) Option {
	return func(c *streamerConfig) {
		c.inputBufferThreshold = n
		c.inputThresholdSet = true
	}
}

// WithOutputBufferThreshold sets the minimum byte count before the output
// audio buffer begins flushing frames into OutputCh. This overrides any
// value derived from the output audio config.
func WithOutputBufferThreshold(n int) Option {
	return func(c *streamerConfig) {
		c.outputBufferThreshold = n
		c.outputThresholdSet = true
	}
}

// WithOutputFrameSize sets the exact output frame size in bytes. This
// overrides any value derived from the output audio config.
func WithOutputFrameSize(n int) Option {
	return func(c *streamerConfig) {
		c.outputFrameSize = n
		c.outputFrameSet = true
	}
}

// WithInputAudioConfig derives the input buffer threshold from the given
// audio config: bytesPerMs(cfg) × DefaultInputDurationMs.
// Ignored if WithInputBufferThreshold is also provided.
func WithInputAudioConfig(cfg *protos.AudioConfig) Option {
	return func(c *streamerConfig) { c.inputAudioConfig = cfg }
}

// WithOutputAudioConfig derives the output frame size and buffer threshold
// from the given audio config: bytesPerMs(cfg) × DefaultFrameDurationMs.
// Ignored if WithOutputFrameSize / WithOutputBufferThreshold are also provided.
func WithOutputAudioConfig(cfg *protos.AudioConfig) Option {
	return func(c *streamerConfig) { c.outputAudioConfig = cfg }
}

// BytesPerMs computes the byte rate per millisecond for the given audio config.
// Formula: sampleRate × bytesPerSample × channels / 1000.
// Returns 0 if cfg is nil.
//
// Delegates to internal_audio.BytesPerMs for the shared implementation.
func BytesPerMs(cfg *protos.AudioConfig) int {
	return internal_audio.BytesPerMs(cfg)
}

// resolveConfig applies all options, then derives any unset thresholds from
// audio configs, then falls back to zero (unbuffered) for anything still unset.
func resolveConfig(opts []Option) streamerConfig {
	cfg := streamerConfig{
		inputChannelSize:  DefaultInputChannelSize,
		outputChannelSize: DefaultOutputChannelSize,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Derive input threshold from audio config if not explicitly set.
	if !cfg.inputThresholdSet && cfg.inputAudioConfig != nil {
		bpm := BytesPerMs(cfg.inputAudioConfig)
		if bpm > 0 {
			cfg.inputBufferThreshold = bpm * DefaultInputDurationMs
		}
	}

	// Derive output frame size and threshold from audio config if not set.
	if !cfg.outputFrameSet && cfg.outputAudioConfig != nil {
		bpm := BytesPerMs(cfg.outputAudioConfig)
		if bpm > 0 {
			cfg.outputFrameSize = bpm * DefaultFrameDurationMs
		}
	}
	if !cfg.outputThresholdSet {
		// Default output threshold = output frame size (flush on first full frame).
		if cfg.outputFrameSize > 0 {
			cfg.outputBufferThreshold = cfg.outputFrameSize
		}
	}

	return cfg
}

// ============================================================================
// BaseStreamer — transport-agnostic channel & buffer management
// ============================================================================

// BaseStreamer owns the input/output channels and audio buffers that every
// concrete streamer (WebRTC, telephony, SIP, …) needs. It handles:
//
//   - InputCh / OutputCh: unified, ordered message channels
//   - inputAudioBuffer / outputAudioBuffer: PCM accumulation with thresholds
//   - FlushAudioCh: interrupt signalling for the output writer
//   - Input / PushOutput: non-blocking type-routed channel sends
//   - ClearInputBuffer / ClearOutputBuffer: buffer + channel draining
//   - Disconnect: idempotent disconnect (returns message for caller to push)
//   - Recv / Context: Streamer interface helpers
//
// The concrete streamer embeds BaseStreamer and only implements
// transport-specific logic (WebRTC track I/O, gRPC dispatch, RTP, etc.).
type BaseStreamer struct {
	Mu sync.Mutex

	// Core components
	Logger commons.Logger

	// Lifecycle
	Ctx    context.Context
	Cancel context.CancelFunc

	// Disconnect tracking — true once Disconnect has run.
	Closed bool

	// Resolved configuration (from options).
	config streamerConfig

	// Three-priority input channels — Recv() drains them in priority order.
	// Mirrors the dispatcher's criticalCh / normalCh / lowCh pattern.
	//
	//   CriticalCh (cap 16)  — disconnection, must-process-now signals
	//   InputCh    (cap cfg) — audio, initialization, configuration, user messages
	//   LowCh      (cap 512) — ConversationEvent observability packets
	//
	// recv (non-blocking) -> CriticalCh|InputCh|LowCh -> Recv() -> Talk loop
	CriticalCh           chan internal_type.Stream
	InputCh              chan internal_type.Stream
	LowCh                chan internal_type.Stream
	inputAudioBuffer     *bytes.Buffer
	inputAudioBufferLock sync.Mutex

	// OutputCh: all upstream-bound messages funnelled here to preserve ordering.
	// send (non-blocking) -> OutputCh -> loop (runOutputWriter) -> upstream service
	OutputCh              chan internal_type.Stream
	outputAudioBuffer     *bytes.Buffer
	outputAudioBufferLock sync.Mutex

	// FlushAudioCh signals the output writer to discard its pending audio queue
	// (used on interruption to silence stale frames immediately).
	FlushAudioCh chan struct{}
}

// NewBaseStreamer initialises a BaseStreamer with channels and buffers sized
// according to the provided options. The streamer owns its own context (derived
// from context.Background) so that cleanup is never short-circuited by the
// caller's context being cancelled first.
//
// Example:
//
//	bs := NewBaseStreamer(logger,
//	    WithOutputAudioConfig(audio.NewLinear48khzMonoAudioConfig()),
//	    WithInputChannelSize(500),
//	    WithOutputChannelSize(1500),
//	)
func NewBaseStreamer(logger commons.Logger, opts ...Option) BaseStreamer {
	cfg := resolveConfig(opts)
	ctx, cancel := context.WithCancel(context.Background())

	// Pre-allocate buffer capacity to avoid internal grow() allocations
	// during streaming. Input buffer holds up to 2× threshold before flush;
	// output buffer holds up to threshold + one extra frame of incoming data.
	inputBufCap := cfg.inputBufferThreshold * 2
	if inputBufCap == 0 {
		inputBufCap = 4096 // safe fallback
	}
	outputBufCap := cfg.outputBufferThreshold + cfg.outputFrameSize
	if outputBufCap == 0 {
		outputBufCap = 4096
	}

	return BaseStreamer{
		Logger:            logger,
		Ctx:               ctx,
		Cancel:            cancel,
		config:            cfg,
		CriticalCh:        make(chan internal_type.Stream, 16),
		InputCh:           make(chan internal_type.Stream, cfg.inputChannelSize),
		LowCh:             make(chan internal_type.Stream, 512),
		OutputCh:          make(chan internal_type.Stream, cfg.outputChannelSize),
		inputAudioBuffer:  bytes.NewBuffer(make([]byte, 0, inputBufCap)),
		outputAudioBuffer: bytes.NewBuffer(make([]byte, 0, outputBufCap)),
		FlushAudioCh:      make(chan struct{}, 1),
	}
}

// ============================================================================
// Input buffer helpers
// ============================================================================

// BufferAndSendInput accumulates resampled audio and sends it to InputCh
// when the buffer reaches the configured input threshold.
//
// Hot-path optimisation: instead of make([]byte)+copy on every flush, we
// swap the filled buffer with a pre-allocated empty one. The old buffer's
// backing array is consumed by the channel reader and eventually GC'd —
// but the swap avoids an explicit copy (the buffer already owns the data).
func (s *BaseStreamer) BufferAndSendInput(audio []byte) {
	s.inputAudioBufferLock.Lock()
	s.inputAudioBuffer.Write(audio)

	if s.inputAudioBuffer.Len() < s.config.inputBufferThreshold {
		s.inputAudioBufferLock.Unlock()
		return
	}

	// Snapshot the accumulated bytes without an extra copy — Bytes() returns
	// a slice backed by the buffer's internal array. We then swap in a fresh
	// buffer so the old backing array is exclusively owned by audioData.
	audioData := s.inputAudioBuffer.Bytes()
	s.inputAudioBuffer = bytes.NewBuffer(make([]byte, 0, s.config.inputBufferThreshold*2))
	s.inputAudioBufferLock.Unlock()

	s.Input(&protos.ConversationUserMessage{
		Message: &protos.ConversationUserMessage_Audio{Audio: audioData},
		Time:    timestamppb.Now(),
	})
}

// ClearInputBuffer resets the input PCM buffer and drains all three priority
// input channels (CriticalCh, InputCh, LowCh).
func (s *BaseStreamer) ClearInputBuffer() {
	s.inputAudioBufferLock.Lock()
	s.inputAudioBuffer.Reset()
	s.inputAudioBufferLock.Unlock()
	for {
		select {
		case <-s.CriticalCh:
		case <-s.InputCh:
		case <-s.LowCh:
		default:
			return
		}
	}
}

// ============================================================================
// Output buffer helpers
// ============================================================================

// BufferAndSendOutput accumulates audio data and flushes consistent frames
// (sized by OutputFrameSize) into OutputCh as ConversationAssistantMessage_Audio
// messages. Encoding (Opus, μ-law, …) is deferred to the concrete streamer's
// output writer.
//
// Hot-path optimisations:
//   - Single lock acquisition: all frames are extracted under one lock, then
//     pushed to the channel outside the lock. This reduces lock contention
//     from N acquires to 1 per call.
//   - sync.Pool frames: frame slices come from a pool and are recycled after
//     the downstream consumer is done (see FrameRelease).
//   - No intermediate copy: bytes.Buffer.Read fills the pooled slice directly.
//
// audio received -> outputAudioBuffer -> check threshold -> flush frames -> OutputCh
func (s *BaseStreamer) BufferAndSendOutput(audio []byte) {
	s.outputAudioBufferLock.Lock()
	s.outputAudioBuffer.Write(audio)

	if s.outputAudioBuffer.Len() < s.config.outputBufferThreshold {
		s.outputAudioBufferLock.Unlock()
		return
	}

	frameSize := s.config.outputFrameSize

	// Collect all complete frames under a single lock acquisition.
	var frames [][]byte
	for s.outputAudioBuffer.Len() >= frameSize {
		frame := getFrame(frameSize)
		s.outputAudioBuffer.Read(frame)
		frames = append(frames, frame)
	}
	s.outputAudioBufferLock.Unlock()

	// Push frames outside the lock — no contention with concurrent writers.
	now := timestamppb.Now()
	for _, frame := range frames {
		s.Output(&protos.ConversationAssistantMessage{
			Message: &protos.ConversationAssistantMessage_Audio{Audio: frame},
			Time:    now,
		})
	}
}

// ClearOutputBuffer resets the output audio buffer, signals the output writer
// to flush its pending audio queue, and drains the output channel.
func (s *BaseStreamer) ClearOutputBuffer() {
	// 1. Reset the audio accumulation buffer so no new frames are produced.
	s.outputAudioBufferLock.Lock()
	s.outputAudioBuffer.Reset()
	s.outputAudioBufferLock.Unlock()

	// 2. Signal the output writer to flush its local pending audio queue first,
	//    before draining OutputCh, to prevent the writer from dequeuing a
	//    message between drain and signal.
	select {
	case s.FlushAudioCh <- struct{}{}:
	default:
	}

	// 3. Drain the output channel (pending audio + other messages).
	for {
		select {
		case <-s.OutputCh:
		default:
			return
		}
	}
}

// ============================================================================
// Synchronous buffer helpers — for transports that handle I/O inline (e.g.
// telephony WebSocket streamers that send audio directly in Send()).
// ============================================================================

// WithInputBuffer executes fn while holding the input buffer lock.
// fn receives the input buffer and its current length.
// Use this for synchronous input accumulation patterns where the concrete
// streamer needs direct buffer access (e.g. telephony WS Recv → buffer → threshold check).
func (s *BaseStreamer) WithInputBuffer(fn func(buf *bytes.Buffer)) {
	s.inputAudioBufferLock.Lock()
	defer s.inputAudioBufferLock.Unlock()
	fn(s.inputAudioBuffer)
}

// WithOutputBuffer executes fn while holding the output buffer lock.
// fn receives the output buffer for direct read/write/flush operations.
// Use this for synchronous output patterns where the concrete streamer sends
// audio chunks inline in Send() rather than through OutputCh.
func (s *BaseStreamer) WithOutputBuffer(fn func(buf *bytes.Buffer)) {
	s.outputAudioBufferLock.Lock()
	defer s.outputAudioBufferLock.Unlock()
	fn(s.outputAudioBuffer)
}

// ResetOutputBuffer resets the output audio buffer under lock.
// Convenience method for interruption handling in synchronous streamers.
func (s *BaseStreamer) ResetOutputBuffer() {
	s.outputAudioBufferLock.Lock()
	s.outputAudioBuffer.Reset()
	s.outputAudioBufferLock.Unlock()
}

// ResetInputBuffer resets the input audio buffer under lock.
func (s *BaseStreamer) ResetInputBuffer() {
	s.inputAudioBufferLock.Lock()
	s.inputAudioBuffer.Reset()
	s.inputAudioBufferLock.Unlock()
}

// ============================================================================
// Channel push helpers
// ============================================================================

// Input routes a message to the correct priority channel based on its type.
//
//	Critical — disconnection, tool call results (must-process-now)
//	Low      — events, metrics, metadata (background observability)
//	Normal   — everything else (audio, initialization, configuration, user messages)
func (s *BaseStreamer) Input(msg internal_type.Stream) {
	switch msg.(type) {
	case *protos.ConversationDisconnection,
		*protos.ConversationToolCallResult:
		select {
		case s.CriticalCh <- msg:
		default:
			s.Logger.Warnw("Critical input channel full, dropping message", "type", fmt.Sprintf("%T", msg))
		}
	case *protos.ConversationEvent,
		*protos.ConversationMetric,
		*protos.ConversationMetadata:
		select {
		case s.LowCh <- msg:
		default:
			s.Logger.Warnw("Low input channel full, dropping message", "type", fmt.Sprintf("%T", msg))
		}
	default:
		select {
		case s.InputCh <- msg:
		default:
			s.Logger.Warnw("Normal input channel full, dropping message", "type", fmt.Sprintf("%T", msg))
		}
	}
}

// PushOutput sends a message to the unified output channel (non-blocking).
func (s *BaseStreamer) Output(msg internal_type.Stream) {
	select {
	case s.OutputCh <- msg:
	default:
		s.Logger.Warnw("Output channel full, dropping message", "type", fmt.Sprintf("%T", msg))
	}
}

// ============================================================================
// Disconnect helpers
// ============================================================================

// Disconnect marks the streamer as closed (idempotent) and returns the
// disconnection message. Returns nil on subsequent calls. The caller is
// responsible for pushing the message via Input().
func (s *BaseStreamer) Disconnect(reason protos.ConversationDisconnection_DisconnectionType) *protos.ConversationDisconnection {
	s.Mu.Lock()
	alreadyClosed := s.Closed
	s.Closed = true
	s.Mu.Unlock()
	if alreadyClosed {
		return nil
	}
	return &protos.ConversationDisconnection{
		Type: reason,
		Time: timestamppb.Now(),
	}
}

// ============================================================================
// Config accessors — let concrete streamers query resolved thresholds
// ============================================================================

// InputBufferThreshold returns the resolved input buffer threshold in bytes.
func (s *BaseStreamer) InputBufferThreshold() int {
	return s.config.inputBufferThreshold
}

// OutputFrameSize returns the resolved output frame size in bytes (one 20 ms frame).
func (s *BaseStreamer) OutputFrameSize() int {
	return s.config.outputFrameSize
}

// OutputBufferThreshold returns the resolved output buffer threshold in bytes.
func (s *BaseStreamer) OutputBufferThreshold() int {
	return s.config.outputBufferThreshold
}

// ============================================================================
// Streamer interface helpers (embedded by concrete streamers)
// ============================================================================

// Context returns the streamer-scoped context.
func (s *BaseStreamer) Context() context.Context {
	return s.Ctx
}

// NotifyMode is a no-op for streamers that do not need mode-specific
// transport setup. Concrete streamers (e.g. webrtcStreamer) override this.
func (s *BaseStreamer) NotifyMode(_ protos.StreamMode) {}

// Recv reads the next downstream-bound message using a two-stage priority select.
// Priority order: CriticalCh > InputCh > LowCh.
//
// Stage 1 (non-blocking): drain CriticalCh first, then InputCh.
// Stage 2 (blocking): wait on all three channels + context cancellation.
//
// This mirrors the dispatcher's priority pattern so that disconnection signals
// always preempt audio, and observability events never delay audio packets.
func (s *BaseStreamer) Recv() (internal_type.Stream, error) {
	// Stage 1: drain critical non-blocking.
	select {
	case msg, ok := <-s.CriticalCh:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	default:
	}

	// Stage 1b: drain normal non-blocking.
	select {
	case msg, ok := <-s.InputCh:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	default:
	}

	// Stage 2: block until any channel has a message or context is cancelled.
	select {
	case msg, ok := <-s.CriticalCh:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case msg, ok := <-s.InputCh:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case msg, ok := <-s.LowCh:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case <-s.Ctx.Done():
		return nil, io.EOF
	}
}
