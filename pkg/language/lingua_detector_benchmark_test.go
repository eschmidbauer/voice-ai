package language

import (
	"testing"

	"github.com/rapidaai/pkg/commons"
)

func BenchmarkDetect_LongEnglishText(b *testing.B) {
	logger, _ := commons.NewApplicationLogger()
	parser := NewLinguaParser(logger)
	text := "This is a longer English paragraph intended to benchmark the language detector under realistic sentence input."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse(text)
	}
}

func BenchmarkDetect_ShortText(b *testing.B) {
	logger, _ := commons.NewApplicationLogger()
	parser := NewLinguaParser(logger)
	text := "hello"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse(text)
	}
}

func BenchmarkDetect_LowAccuracyMode(b *testing.B) {
	logger, _ := commons.NewApplicationLogger()
	parser := NewLinguaParser(logger)
	text := "Bonjour, this mixed sentence is for benchmark checks only."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse(text)
	}
}
