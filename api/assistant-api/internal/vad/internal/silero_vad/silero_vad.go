// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_silero_vad

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
)

// -----------------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------------

const (
	// vadName is the identifier for this VAD implementation
	vadName = "silero_vad"

	// Default configuration values — aligned with FireRedVAD defaults
	// (20 frames × 10 ms = 200 ms silence, 8 frames × 10 ms = 80 ms pad)
	defaultThreshold            = 0.5
	defaultMinSilenceDurationMs = 200
	defaultSpeechPadMs          = 80

	// Environment variable for model path
	envModelPathKey = "SILERO_MODEL_PATH"

	// Default model filename
	defaultModelFile = "models/silero_vad_20251001.onnx"
)

// -----------------------------------------------------------------------------
// SileroVAD - Voice Activity Detection using Silero
// -----------------------------------------------------------------------------

// SileroVAD implements the Vad interface using the Silero ONNX model
// with native ONNX Runtime inference. It provides thread-safe voice
// activity detection with automatic cleanup on context cancellation.
//
// Input audio is expected to be 16 kHz LINEAR16 mono (the platform's
// internal audio format). No resampling is performed.
type SileroVAD struct {
	// Core dependencies
	logger   commons.Logger
	onPacket func(ctx context.Context, pkt ...internal_type.Packet) error

	// Silero detector (CGO-backed, requires careful lifecycle management)
	detector *Detector

	// Thread-safety for CGO resource protection
	mu           sync.RWMutex
	isTerminated bool
}

// -----------------------------------------------------------------------------
// Constructor
// -----------------------------------------------------------------------------

// NewSileroVAD creates a new SileroVAD instance.
// Input audio must be 16 kHz LINEAR16 mono — the platform's internal format.
// The VAD will automatically close when the provided context is cancelled,
// ensuring safe cleanup of CGO resources.
func NewSileroVAD(
	ctx context.Context,
	logger commons.Logger,
	onPacket func(ctx context.Context, pkt ...internal_type.Packet) error,
	options utils.Option,
) (internal_type.Vad, error) {
	start := time.Now()

	// Initialize detector
	detector, err := createDetector(options)
	if err != nil {
		return nil, fmt.Errorf("failed to create silero detector: %w", err)
	}

	svad := &SileroVAD{
		logger:       logger,
		onPacket:     onPacket,
		detector:     detector,
		isTerminated: false,
	}

	// Start lifecycle manager for automatic cleanup
	svad.startLifecycleManager(ctx)

	if onPacket != nil {
		_ = onPacket(ctx, internal_type.ConversationEventPacket{
			Name: "vad",
			Data: map[string]string{
				"type":     "initialized",
				"provider": vadName,
				"init_ms":  fmt.Sprintf("%d", time.Since(start).Milliseconds()),
			},
			Time: time.Now(),
		})
	}

	return svad, nil
}

// -----------------------------------------------------------------------------
// Public Interface Methods
// -----------------------------------------------------------------------------

// Name returns the identifier for this VAD implementation.
func (s *SileroVAD) Name() string {
	return vadName
}

// Process analyzes an audio packet for voice activity.
// The packet must contain 16 kHz LINEAR16 mono audio.
// Returns immediately if the VAD has been terminated.
// Thread-safe for concurrent calls.
func (s *SileroVAD) Process(ctx context.Context, pkt internal_type.UserAudioPacket) error {
	// Early termination check
	if !s.isActive() {
		return nil
	}

	// Convert LINEAR16 bytes to float32 samples
	samples := linear16ToFloat32(pkt.Audio)

	// Perform detection with CGO safety
	segments, isSpeaking, err := s.detectSafely(samples)
	if err != nil {
		return err
	}

	// Emit InterruptionPacket only on confirmed speech onset — this is the
	// signal to interrupt assistant TTS/LLM.
	if hasSpeechStart(segments) {
		s.notifyActivity(ctx, segments)
	}

	// Emit a heartbeat while the user is actively speaking so the EOS
	// silence timer keeps extending during sustained speech.
	if isSpeaking && s.onPacket != nil {
		_ = s.onPacket(ctx,
			internal_type.VadSpeechActivityPacket{},
			internal_type.ConversationEventPacket{
				Name: "vad",
				Data: map[string]string{
					"type": "heartbeat",
				},
			})
	}

	return nil
}

// Close terminates the VAD and releases all CGO resources.
// Safe to call multiple times; subsequent calls are no-ops.
// Thread-safe.
func (s *SileroVAD) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isTerminated {
		return nil
	}
	s.isTerminated = true

	if s.detector != nil {
		s.detector.Destroy()
		s.detector = nil
	}

	return nil
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Initialization
// -----------------------------------------------------------------------------

// createDetector initializes the Silero speech detector with configuration.
func createDetector(options utils.Option) (*Detector, error) {
	modelPath := resolveModelPath()
	threshold := resolveThreshold(options)

	config := DetectorConfig{
		ModelPath:            modelPath,
		SampleRate:           16000, // Silero requires 16kHz
		Threshold:            float32(threshold),
		MinSilenceDurationMs: resolveMinSilenceDurationMs(options),
		SpeechPadMs:          resolveSpeechPadMs(options),
	}
	return NewDetector(config)
}

// resolveModelPath determines the ONNX model file path.
func resolveModelPath() string {
	if envPath := os.Getenv(envModelPathKey); envPath != "" {
		return envPath
	}

	_, currentFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(currentFile), defaultModelFile)
}

// resolveThreshold extracts threshold from options or returns default.
func resolveThreshold(options utils.Option) float64 {
	if options == nil {
		return defaultThreshold
	}

	if threshold, err := options.GetFloat64("microphone.vad.threshold"); err == nil {
		return threshold
	}

	return defaultThreshold
}

// resolveMinSilenceDurationMs extracts min silence duration from options.
// The option key uses frame count (consistent with FireRedVAD config);
// each frame is 10 ms, so we multiply by 10 to get milliseconds.
func resolveMinSilenceDurationMs(options utils.Option) int {
	if options == nil {
		return defaultMinSilenceDurationMs
	}
	if v, err := options.GetFloat64("microphone.vad.min_silence_frame"); err == nil {
		return int(v) * 10
	}
	return defaultMinSilenceDurationMs
}

// resolveSpeechPadMs extracts speech pad duration from options.
// The option key uses frame count (consistent with FireRedVAD config);
// each frame is 10 ms, so we multiply by 10 to get milliseconds.
func resolveSpeechPadMs(options utils.Option) int {
	if options == nil {
		return defaultSpeechPadMs
	}
	if v, err := options.GetFloat64("microphone.vad.min_speech_frame"); err == nil {
		return int(v) * 10
	}
	return defaultSpeechPadMs
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Lifecycle
// -----------------------------------------------------------------------------

// hasSpeechStart returns true if any segment contains a speech onset.
func hasSpeechStart(segments []Segment) bool {
	for _, seg := range segments {
		if seg.SpeechStartAt > 0 {
			return true
		}
	}
	return false
}

// startLifecycleManager spawns a goroutine that closes the VAD
// when the context is cancelled.
func (s *SileroVAD) startLifecycleManager(ctx context.Context) {
	go func() {
		<-ctx.Done()
		s.Close()
	}()
}

// isActive checks if the VAD is still operational.
// Thread-safe.
func (s *SileroVAD) isActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.isTerminated && s.detector != nil
}

// -----------------------------------------------------------------------------
// Private Helper Methods - Audio Processing
// -----------------------------------------------------------------------------

// linear16ToFloat32 converts signed 16-bit little-endian PCM bytes to
// float32 samples in the range [-1.0, 1.0]. This is the only conversion
// needed since input is always 16 kHz LINEAR16 mono.
func linear16ToFloat32(data []byte) []float32 {
	numSamples := len(data) / 2
	samples := make([]float32, numSamples)
	for i := 0; i < numSamples; i++ {
		sample := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		samples[i] = float32(sample) / 32768.0
	}
	return samples
}

// detectSafely performs voice activity detection with CGO resource protection.
// Holds the write lock for the duration of the CGO call: Detector
// mutates internal ONNX state and is not safe for concurrent use.
// Returns segments and whether the detector is currently in a triggered (speech active) state.
func (s *SileroVAD) detectSafely(samples []float32) ([]Segment, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isTerminated || s.detector == nil {
		return nil, false, nil
	}

	segments, err := s.detector.Detect(samples)
	if err != nil {
		return nil, false, fmt.Errorf("detection failed: %w", err)
	}

	return segments, s.detector.triggered, nil
}

// notifyActivity calculates speech boundaries and invokes the callback.
func (s *SileroVAD) notifyActivity(ctx context.Context, segments []Segment) {
	minStart := math.MaxFloat64
	maxEnd := -math.MaxFloat64

	for _, seg := range segments {
		start := float64(seg.SpeechStartAt)
		end := float64(seg.SpeechEndAt)

		if start < minStart {
			minStart = start
		}
		if end > maxEnd {
			maxEnd = end
		}
	}

	if s.onPacket != nil {
		s.onPacket(ctx,
			internal_type.InterruptionPacket{
				Source:  internal_type.InterruptionSourceVad,
				StartAt: minStart,
				EndAt:   maxEnd,
			},
			internal_type.ConversationEventPacket{
				Name: "vad",
				Data: map[string]string{
					"type":          "detected",
					"start_at":      fmt.Sprintf("%f", minStart),
					"end_at":        fmt.Sprintf("%f", maxEnd),
					"segment_count": fmt.Sprintf("%d", len(segments)),
				},
			},
		)
	}
}
