// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.
package internal_input_normalizers

import (
	"context"
	"errors"
	"testing"

	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	rapida_types "github.com/rapidaai/pkg/types"
)

type parserStub struct {
	calls int
	out   *rapida_types.Language
	conf  float64
}

func (p *parserStub) Parse(_ string) (*rapida_types.Language, float64) {
	p.calls++
	return p.out, p.conf
}

type unknownPipeline struct {
	stop bool
}

func (p *unknownPipeline) IsStop() bool {
	return p.stop
}

func newTestNormalizer(t *testing.T, onPacket func(...internal_type.Packet) error) *inputNormalizer {
	t.Helper()
	logger, _ := commons.NewApplicationLogger()
	n := NewInputNormalizer(logger).(*inputNormalizer)
	if err := n.Initialize(context.Background(), onPacket); err != nil {
		t.Fatalf("unexpected initialize error: %v", err)
	}
	return n
}

func mustLanguage(t *testing.T, code string) *rapida_types.Language {
	t.Helper()
	lang := rapida_types.LookupLanguage(code)
	if lang == nil {
		t.Fatalf("language %q not found", code)
	}
	return lang
}

func TestInputNormalizer_Normalize_EndOfSpeechBuildsNormalizedUserTextPacket(t *testing.T) {
	emitted := make([]internal_type.Packet, 0)
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted = append(emitted, pkts...)
		return nil
	})
	packets := []internal_type.Packet{
		internal_type.EndOfSpeechPacket{
			ContextID: "ctx-1",
			Speech:    "hello there",
			Speechs: []internal_type.SpeechToTextPacket{
				{ContextID: "ctx-1", Script: "hello", Language: "en"},
				{ContextID: "ctx-1", Script: "there", Language: "en-US"},
				{ContextID: "ctx-1", Script: "bonjour", Language: "fr"},
			},
		},
	}
	if err := n.Normalize(context.Background(), packets...); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(emitted) != 1 {
		t.Fatalf("expected one emitted packet, got %d", len(emitted))
	}
	out, ok := emitted[0].(internal_type.NormalizedUserTextPacket)
	if !ok {
		t.Fatalf("expected NormalizedUserTextPacket, got %T", emitted[0])
	}
	if out.ContextID != "ctx-1" {
		t.Fatalf("expected context ctx-1, got %q", out.ContextID)
	}
	if out.Text != "hello there" {
		t.Fatalf("expected speech text preserved, got %q", out.Text)
	}
	if out.Language.ISO639_1 != "en" {
		t.Fatalf("expected consensus language en, got %q", out.Language.ISO639_1)
	}
}

func TestInputNormalizer_Normalize_UserTextUsesParserWhenNoChunkLanguage(t *testing.T) {
	parser := &parserStub{out: mustLanguage(t, "es"), conf: 0.99}
	emitted := make([]internal_type.Packet, 0)
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted = append(emitted, pkts...)
		return nil
	})
	n.parser = parser

	if err := n.Normalize(context.Background(), internal_type.UserTextPacket{ContextID: "ctx-2", Text: "hola como estas"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parser.calls != 1 {
		t.Fatalf("expected parser called once, got %d", parser.calls)
	}
	out := emitted[0].(internal_type.NormalizedUserTextPacket)
	if out.Language.ISO639_1 != "es" {
		t.Fatalf("expected parser language es, got %q", out.Language.ISO639_1)
	}
}

func TestInputNormalizer_Normalize_UserTextUsesProvidedLanguage(t *testing.T) {
	parser := &parserStub{out: mustLanguage(t, "en"), conf: 0.99}
	emitted := make([]internal_type.Packet, 0)
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted = append(emitted, pkts...)
		return nil
	})
	n.parser = parser

	if err := n.Normalize(context.Background(), internal_type.UserTextPacket{ContextID: "ctx-2", Text: "hola como estas", Language: "fr"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parser.calls != 0 {
		t.Fatalf("expected parser not called when language present, got %d", parser.calls)
	}
	out := emitted[0].(internal_type.NormalizedUserTextPacket)
	if out.Language.ISO639_1 != "fr" {
		t.Fatalf("expected canonical language fr, got %q", out.Language.ISO639_1)
	}
}

func TestInputNormalizer_Normalize_UnknownChunkLanguageDoesNotBiasEnglish(t *testing.T) {
	parser := &parserStub{out: mustLanguage(t, "es"), conf: 0.99}
	emitted := make([]internal_type.Packet, 0)
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted = append(emitted, pkts...)
		return nil
	})
	n.parser = parser

	err := n.Normalize(context.Background(), internal_type.EndOfSpeechPacket{
		ContextID: "ctx-3",
		Speech:    "bonjour",
		Speechs: []internal_type.SpeechToTextPacket{
			{ContextID: "ctx-3", Script: "??", Language: "xx"},
			{ContextID: "ctx-3", Script: "bonjour", Language: "fr-FR"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parser.calls != 0 {
		t.Fatalf("expected parser not called when valid chunk language exists, got %d", parser.calls)
	}

	out := emitted[0].(internal_type.NormalizedUserTextPacket)
	if out.Language.ISO639_1 != "fr" {
		t.Fatalf("expected language fr, got %q", out.Language.ISO639_1)
	}
}

func TestInputNormalizer_Normalize_ParserNoMatchLeavesLanguageUnset(t *testing.T) {
	parser := &parserStub{out: nil}
	emitted := make([]internal_type.Packet, 0)
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted = append(emitted, pkts...)
		return nil
	})
	n.parser = parser

	if err := n.Normalize(context.Background(), internal_type.UserTextPacket{ContextID: "ctx-4", Text: "??"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parser.calls != 1 {
		t.Fatalf("expected parser called once, got %d", parser.calls)
	}
	out := emitted[0].(internal_type.NormalizedUserTextPacket)
	if out.Language != nil {
		t.Fatalf("expected nil language when parser has no match, got %q", out.Language.ISO639_1)
	}
}

func TestInputNormalizer_Pipeline_StopAtInput(t *testing.T) {
	emitted := 0
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted += len(pkts)
		return nil
	})
	err := n.Pipeline(context.Background(), InputPipeline{PipelinePacket: PipelinePacket{Stop: true, ContextID: "ctx"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emitted != 0 {
		t.Fatalf("expected no emissions, got %d", emitted)
	}
}

func TestInputNormalizer_Pipeline_StopAtProcess(t *testing.T) {
	emitted := 0
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted += len(pkts)
		return nil
	})
	err := n.Pipeline(context.Background(), DetectLanguageProcessPipeline{ProcessPipeline: ProcessPipeline{PipelinePacket: PipelinePacket{Stop: true, ContextID: "ctx", Speech: "hello"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emitted != 0 {
		t.Fatalf("expected no emissions, got %d", emitted)
	}
}

func TestInputNormalizer_Pipeline_StopAtOutput(t *testing.T) {
	emitted := 0
	n := newTestNormalizer(t, func(pkts ...internal_type.Packet) error {
		emitted += len(pkts)
		return nil
	})
	err := n.Pipeline(context.Background(), OutputPipeline{PipelinePacket: PipelinePacket{Stop: true, ContextID: "ctx", Speech: "hello"}, Language: mustLanguage(t, "en")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emitted != 0 {
		t.Fatalf("expected no emissions, got %d", emitted)
	}
}

func TestInputNormalizer_Pipeline_OutputPropagatesOnPacketError(t *testing.T) {
	errExpected := errors.New("on packet failed")
	n := newTestNormalizer(t, func(...internal_type.Packet) error {
		return errExpected
	})
	err := n.Pipeline(context.Background(), OutputPipeline{PipelinePacket: PipelinePacket{ContextID: "ctx", Speech: "hello"}, Language: mustLanguage(t, "en")})
	if !errors.Is(err, errExpected) {
		t.Fatalf("expected onPacket error %v, got %v", errExpected, err)
	}
}

func TestInputNormalizer_Pipeline_RejectsUnsupportedPipelineType(t *testing.T) {
	n := newTestNormalizer(t, nil)
	err := n.Pipeline(context.Background(), &unknownPipeline{})
	if err == nil {
		t.Fatalf("expected unsupported pipeline type error")
	}
}
