// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_asterisk

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	internal_audio_resampler "github.com/rapidaai/api/assistant-api/internal/audio/resampler"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/protos"
)

const (
	chunkDuration        = 20 * time.Millisecond
	defaultFrameSize     = 160
	inputBufferThreshold = 32 * 60
)

// AudioChunk represents a processed audio chunk ready for streaming.
type AudioChunk struct {
	Data     []byte
	Duration time.Duration
}

// AudioProcessorConfig parameterizes the shared AudioProcessor for different
// Asterisk transports (AudioSocket SLIN vs WebSocket mu-law).
type AudioProcessorConfig struct {
	AsteriskConfig   *protos.AudioConfig // provider-side format
	DownstreamConfig *protos.AudioConfig // internal format (linear16 16kHz)
	SilenceByte      byte                // 0xFF for mu-law, 0x00 for SLIN
	FrameSize        int                 // optimal frame size (bytes per 20ms)
}

// AudioProcessor handles audio conversion between Asterisk and downstream
// formats. It is parameterized by AudioProcessorConfig so both the AudioSocket
// (SLIN 8kHz) and WebSocket (mu-law 8kHz) transports share a single
// implementation.
type AudioProcessor struct {
	logger           commons.Logger
	resampler        internal_type.AudioResampler
	asteriskConfig   *protos.AudioConfig
	downstreamConfig *protos.AudioConfig
	silenceByte      byte
	optimalFrameSize int

	inputBuffer   *bytes.Buffer
	inputBufferMu sync.Mutex
	outputBuffer   *bytes.Buffer
	outputBufferMu sync.Mutex

	onInputAudio  func(audio []byte)
	onOutputChunk func(chunk *AudioChunk) error
	silenceChunk  *AudioChunk

	xoffActive bool
	xoffMu     sync.Mutex
}

func NewAudioProcessor(logger commons.Logger, cfg AudioProcessorConfig) (*AudioProcessor, error) {
	resampler, err := internal_audio_resampler.GetResampler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create resampler: %w", err)
	}

	frameSize := cfg.FrameSize
	if frameSize <= 0 {
		frameSize = defaultFrameSize
	}

	p := &AudioProcessor{
		logger:           logger,
		resampler:        resampler,
		asteriskConfig:   cfg.AsteriskConfig,
		downstreamConfig: cfg.DownstreamConfig,
		silenceByte:      cfg.SilenceByte,
		optimalFrameSize: frameSize,
		inputBuffer:      new(bytes.Buffer),
		outputBuffer:     new(bytes.Buffer),
	}
	p.silenceChunk = p.createSilenceChunk()
	return p, nil
}

func (p *AudioProcessor) SetInputAudioCallback(callback func(audio []byte)) {
	p.onInputAudio = callback
}

func (p *AudioProcessor) SetOutputChunkCallback(callback func(chunk *AudioChunk) error) {
	p.onOutputChunk = callback
}

func (p *AudioProcessor) SetOptimalFrameSize(size int) {
	if size > 0 {
		p.optimalFrameSize = size
		p.silenceChunk = p.createSilenceChunk()
	}
}

func (p *AudioProcessor) GetOptimalFrameSize() int {
	return p.optimalFrameSize
}

func (p *AudioProcessor) GetDownstreamConfig() *protos.AudioConfig {
	return p.downstreamConfig
}

func (p *AudioProcessor) ProcessInputAudio(audio []byte) error {
	if len(audio) == 0 {
		return nil
	}

	converted, err := p.resampler.Resample(audio, p.asteriskConfig, p.downstreamConfig)
	if err != nil {
		return fmt.Errorf("audio conversion from asterisk format to downstream failed: %w", err)
	}

	p.bufferAndSendInput(converted)
	return nil
}

func (p *AudioProcessor) bufferAndSendInput(audio []byte) {
	p.inputBufferMu.Lock()
	p.inputBuffer.Write(audio)

	if p.inputBuffer.Len() < inputBufferThreshold {
		p.inputBufferMu.Unlock()
		return
	}

	audioData := make([]byte, p.inputBuffer.Len())
	p.inputBuffer.Read(audioData)
	p.inputBufferMu.Unlock()

	if p.onInputAudio != nil {
		p.onInputAudio(audioData)
	}
}

func (p *AudioProcessor) ClearInputBuffer() {
	p.inputBufferMu.Lock()
	p.inputBuffer.Reset()
	p.inputBufferMu.Unlock()
}

func (p *AudioProcessor) ProcessOutputAudio(audio []byte) error {
	if len(audio) == 0 {
		return nil
	}

	converted, err := p.resampler.Resample(audio, p.downstreamConfig, p.asteriskConfig)
	if err != nil {
		return fmt.Errorf("audio conversion from downstream to asterisk format failed: %w", err)
	}

	p.outputBufferMu.Lock()
	p.outputBuffer.Write(converted)
	p.outputBufferMu.Unlock()

	return nil
}

func (p *AudioProcessor) GetNextChunk() *AudioChunk {
	chunkSize := p.optimalFrameSize
	if chunkSize <= 0 {
		chunkSize = defaultFrameSize
	}

	chunk := make([]byte, chunkSize)

	p.outputBufferMu.Lock()
	n, _ := p.outputBuffer.Read(chunk)
	p.outputBufferMu.Unlock()

	if n == 0 {
		return nil
	}

	if n < chunkSize {
		for i := n; i < chunkSize; i++ {
			chunk[i] = p.silenceByte
		}
	}

	return &AudioChunk{
		Data:     chunk,
		Duration: chunkDuration,
	}
}

func (p *AudioProcessor) ClearOutputBuffer() {
	p.outputBufferMu.Lock()
	p.outputBuffer.Reset()
	p.outputBufferMu.Unlock()
}

func (p *AudioProcessor) createSilenceChunk() *AudioChunk {
	chunkSize := p.optimalFrameSize
	if chunkSize <= 0 {
		chunkSize = defaultFrameSize
	}

	chunk := make([]byte, chunkSize)
	for i := range chunk {
		chunk[i] = p.silenceByte
	}

	return &AudioChunk{
		Data:     chunk,
		Duration: chunkDuration,
	}
}

func (p *AudioProcessor) RunOutputSender(ctx context.Context) {
	if p.onOutputChunk == nil {
		p.logger.Error("RunOutputSender called without output callback set")
		return
	}

	nextSendTime := time.Now().Add(chunkDuration)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		now := time.Now()
		if sleepDuration := nextSendTime.Sub(now); sleepDuration > 0 {
			time.Sleep(sleepDuration)
		}

		nextSendTime = nextSendTime.Add(chunkDuration)
		if time.Now().After(nextSendTime) {
			nextSendTime = time.Now().Add(chunkDuration)
		}

		if p.IsXOFF() {
			continue
		}

		chunk := p.GetNextChunk()
		if chunk == nil {
			chunk = p.silenceChunk
		}

		if err := p.onOutputChunk(chunk); err != nil {
			p.logger.Debug("Failed to send audio chunk", "error", err)
		}
	}
}

func (p *AudioProcessor) SetXOFF() {
	p.xoffMu.Lock()
	p.xoffActive = true
	p.xoffMu.Unlock()
}

func (p *AudioProcessor) SetXON() {
	p.xoffMu.Lock()
	p.xoffActive = false
	p.xoffMu.Unlock()
}

func (p *AudioProcessor) IsXOFF() bool {
	p.xoffMu.Lock()
	defer p.xoffMu.Unlock()
	return p.xoffActive
}
