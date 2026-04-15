// Copyright (c) 2023-2026 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_audio

import (
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/protos"
	"github.com/zaf/g711"
)

// BytesPerSample returns the number of bytes per audio sample for the given
// audio format. Returns 0 for unsupported formats.
func BytesPerSample(format protos.AudioConfig_AudioFormat) int {
	switch format {
	case protos.AudioConfig_LINEAR16:
		return 2
	case protos.AudioConfig_MuLaw8:
		return 1
	default:
		return 0
	}
}

// BytesPerMs computes the byte rate per millisecond for the given audio config.
// Formula: sampleRate × bytesPerSample × channels / 1000.
// Returns 0 if cfg is nil or the format is unsupported.
func BytesPerMs(cfg *protos.AudioConfig) int {
	if cfg == nil {
		return 0
	}
	return int(cfg.GetSampleRate()) * BytesPerSample(cfg.GetAudioFormat()) * int(cfg.GetChannels()) / 1000
}

// BytesPerSecond computes the byte rate per second for the given audio config.
// Formula: sampleRate × bytesPerSample × channels.
// Returns 0 if cfg is nil or the format is unsupported.
func BytesPerSecond(cfg *protos.AudioConfig) int {
	if cfg == nil {
		return 0
	}
	return int(cfg.GetSampleRate()) * BytesPerSample(cfg.GetAudioFormat()) * int(cfg.GetChannels())
}

// FrameSize returns the number of bytes in a single audio frame (all channels)
// for the given audio config. Returns 0 if cfg is nil or the format is unsupported.
func FrameSize(cfg *protos.AudioConfig) int {
	if cfg == nil {
		return 0
	}
	return BytesPerSample(cfg.GetAudioFormat()) * int(cfg.GetChannels())
}

// AlawToUlaw converts A-law (PCMA) encoded audio to µ-law (PCMU).
func AlawToUlaw(data []byte) []byte {
	return g711.Alaw2Ulaw(data)
}

// UlawToAlaw converts µ-law (PCMU) encoded audio to A-law (PCMA).
func UlawToAlaw(data []byte) []byte {
	return g711.EncodeAlaw(g711.DecodeUlaw(data))
}

// GetAudioInfo returns detailed information about raw audio data based on
// the provided audio config. The returned AudioInfo.DurationMs contains the
// audio duration in milliseconds for sub-second granularity.
func GetAudioInfo(data []byte, config *protos.AudioConfig) internal_type.AudioInfo {
	bps := BytesPerSample(config.GetAudioFormat())
	channels := int(config.GetChannels())

	var samplesPerChannel int
	if bps > 0 && channels > 0 {
		samplesPerChannel = len(data) / (bps * channels)
	}

	durationMs := float64(samplesPerChannel) / float64(config.GetSampleRate()) * 1000.0

	return internal_type.AudioInfo{
		SampleRate:        config.GetSampleRate(),
		Format:            config.GetAudioFormat(),
		Channels:          config.GetChannels(),
		SamplesPerChannel: samplesPerChannel,
		BytesPerSample:    bps,
		TotalBytes:        len(data),
		DurationMs:        durationMs,
	}
}
