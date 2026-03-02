// Copyright (c) 2023-2025 RapidaAI
// Author: Prashant Srivastav <prashant@rapida.ai>
//
// Licensed under GPL-2.0 with Rapida Additional Terms.
// See LICENSE.md or contact sales@rapida.ai for commercial usage.

package internal_transformer_rime

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	internal_normalizers "github.com/rapidaai/api/assistant-api/internal/normalizers"
	internal_type "github.com/rapidaai/api/assistant-api/internal/type"
	"github.com/rapidaai/pkg/commons"
	"github.com/rapidaai/pkg/utils"
)

// rimeNormalizer handles Rime TTS text preprocessing.
// Rime does NOT support SSML. Custom pauses use <ms> syntax (e.g., <500> for 500ms pause).
type rimeNormalizer struct {
	logger   commons.Logger
	config   internal_type.NormalizerConfig
	language string

	normalizers []internal_normalizers.Normalizer

	conjunctionPattern *regexp.Regexp
}

// NewRimeNormalizer creates a Rime-specific text normalizer.
func NewRimeNormalizer(logger commons.Logger, opts utils.Option) internal_type.TextNormalizer {
	cfg := internal_type.DefaultNormalizerConfig()

	language, _ := opts.GetString("speaker.language")
	if language == "" {
		language = "eng"
	}

	var conjunctionPattern *regexp.Regexp
	if conjunctionBoundaries, err := opts.GetString("speaker.conjunction.boundaries"); err == nil && conjunctionBoundaries != "" {
		cfg.Conjunctions = strings.Split(conjunctionBoundaries, commons.SEPARATOR)

		escaped := make([]string, len(cfg.Conjunctions))
		for i, c := range cfg.Conjunctions {
			escaped[i] = regexp.QuoteMeta(strings.TrimSpace(c))
		}
		pattern := `(` + strings.Join(escaped, "|") + `)`
		conjunctionPattern = regexp.MustCompile(pattern)
	}

	if conjunctionBreak, err := opts.GetUint64("speaker.conjunction.break"); err == nil {
		cfg.PauseDurationMs = conjunctionBreak
	}

	var normalizers []internal_normalizers.Normalizer
	if dictionaries, err := opts.GetString("speaker.pronunciation.dictionaries"); err == nil && dictionaries != "" {
		normalizerNames := strings.Split(dictionaries, commons.SEPARATOR)
		normalizers = internal_type.BuildNormalizerPipeline(logger, normalizerNames)
	}

	return &rimeNormalizer{
		logger:             logger,
		config:             cfg,
		language:           language,
		normalizers:        normalizers,
		conjunctionPattern: conjunctionPattern,
	}
}

// Normalize applies Rime-specific text transformations.
// Rime uses <ms> syntax for pauses instead of SSML.
func (n *rimeNormalizer) Normalize(ctx context.Context, text string) string {
	if text == "" {
		return text
	}

	text = n.removeMarkdown(text)

	for _, normalizer := range n.normalizers {
		text = normalizer.Normalize(text)
	}

	// Insert breaks after conjunction boundaries using Rime's <ms> pause syntax
	if n.conjunctionPattern != nil && n.config.PauseDurationMs > 0 {
		text = n.insertConjunctionBreaks(text)
	}

	return n.normalizeWhitespace(text)
}

func (n *rimeNormalizer) removeMarkdown(input string) string {
	re := regexp.MustCompile(`(?m)^#{1,6}\s*`)
	output := re.ReplaceAllString(input, "")

	re = regexp.MustCompile(`\*{1,2}([^*]+?)\*{1,2}|_{1,2}([^_]+?)_{1,2}`)
	output = re.ReplaceAllString(output, "$1$2")

	re = regexp.MustCompile("`([^`]+)`")
	output = re.ReplaceAllString(output, "$1")

	re = regexp.MustCompile("(?s)```[^`]*```")
	output = re.ReplaceAllString(output, "")

	re = regexp.MustCompile(`(?m)^>\s?`)
	output = re.ReplaceAllString(output, "")

	re = regexp.MustCompile(`\[(.*?)\]\(.*?\)`)
	output = re.ReplaceAllString(output, "$1")

	re = regexp.MustCompile(`!\[(.*?)\]\(.*?\)`)
	output = re.ReplaceAllString(output, "$1")

	re = regexp.MustCompile(`(?m)^(-{3,}|\*{3,}|_{3,})$`)
	output = re.ReplaceAllString(output, "")

	re = regexp.MustCompile(`[*_]+`)
	output = re.ReplaceAllString(output, "")

	return output
}

// insertConjunctionBreaks adds pauses after conjunctions using Rime's <ms> syntax.
func (n *rimeNormalizer) insertConjunctionBreaks(text string) string {
	pauseTag := fmt.Sprintf(" <%d> ", n.config.PauseDurationMs)

	return n.conjunctionPattern.ReplaceAllStringFunc(text, func(match string) string {
		return match + pauseTag
	})
}

func (n *rimeNormalizer) normalizeWhitespace(text string) string {
	re := regexp.MustCompile(`\s+`)
	result := re.ReplaceAllString(text, " ")
	return strings.TrimSpace(result)
}
