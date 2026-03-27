package language

import (
	rapida_types "github.com/rapidaai/pkg/types"
)

// Parser follows the same Parse-style contract used across pkg/parsers.
// Parse returns canonical rapida language metadata for the given input text.
// nil indicates no reliable canonical language could be resolved.
type Parser interface {
	Parse(text string) (*rapida_types.Language, float64)
}
