// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_input_normalizers

import (
	"context"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/types"
)

const (
	LanguageAttributeISO6391 = "language.iso639_1"
	LanguageAttributeISO6392 = "language.iso639_2"
	LanguageAttributeName    = "language.name"
	LanguageAttributeSource  = "language.source"
)

// PipelineType matches the model-executor pattern: each packet type controls the next transition.
type PipelineType interface {
	IsStop() bool
}

// PipelinePacket carries shared state across normalization pipeline stages.
type PipelinePacket struct {
	PipelineType

	// stop signals the pipeline to stop processing and emit no output.
	Stop bool

	//
	ContextID string

	// speech
	Speech string

	// language attributes for language detection/canonicalization stages to populate and downstream stages to consume.
	Speechs []internal_type.SpeechToTextPacket
}

func (p PipelinePacket) IsStop() bool {
	return p.Stop
}

// InputPipeline is the first stage for each input packet.
type InputPipeline struct {
	PipelinePacket
}

// ProcessPipeline is the common base for normalization process stages.
type ProcessPipeline struct {
	PipelinePacket
}

// DetectLanguageProcessPipeline canonicalizes/detects language on user text packets.
type DetectLanguageProcessPipeline struct {
	ProcessPipeline
}

// OutputPipeline emits normalized packet to downstream OnPacket callback.
type OutputPipeline struct {
	PipelinePacket
	Language *types.Language
}

// InputNormalizer normalizes input packets and emits them via OnPacket at OutputPipeline.
type InputNormalizer interface {
	Initialize(ctx context.Context, onPacket func(...internal_type.Packet) error) error
	Normalize(ctx context.Context, packets ...internal_type.Packet) error
	Close(ctx context.Context) error
}
