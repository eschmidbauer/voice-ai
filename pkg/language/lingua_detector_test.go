package language

import (
	"testing"

	"github.com/rapidaai/pkg/commons"
)

func TestParse_EmptyInputDefaultsToEnglish(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	parser := NewLinguaParser(logger)
	res, confidence := parser.Parse("   ")
	if res != nil {
		t.Fatalf("expected no detection for empty input, got %+v", res)
	}
	if confidence != 0 {
		t.Fatalf("expected zero confidence for empty input, got %f", confidence)
	}
}

func TestParse_English(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	parser := NewLinguaParser(logger)
	res, confidence := parser.Parse("Hello there, how are you doing today?")
	if res == nil {
		t.Fatalf("expected successful detection")
	}
	if res.ISO639_1 != "en" {
		t.Fatalf("expected en, got %q", res.ISO639_1)
	}
	if res.ISO639_2 != "eng" {
		t.Fatalf("expected eng, got %q", res.ISO639_2)
	}
	if confidence <= 0 {
		t.Fatalf("expected confidence > 0, got %f", confidence)
	}
}

func TestParse_French(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	parser := NewLinguaParser(logger)
	res, _ := parser.Parse("Bonjour tout le monde, comment allez-vous?")
	if res == nil {
		t.Fatalf("expected successful detection")
	}
	if res.ISO639_1 != "fr" {
		t.Fatalf("expected fr, got %q", res.ISO639_1)
	}
	if res.ISO639_2 != "fra" {
		t.Fatalf("expected fra, got %q", res.ISO639_2)
	}
}

func TestParse_WithLowAccuracyMode(t *testing.T) {
	logger, _ := commons.NewApplicationLogger()
	parser := NewLinguaParser(logger)
	res, _ := parser.Parse("Hola, esto es una prueba corta")
	if res == nil {
		t.Fatalf("expected successful detection")
	}
	if res.ISO639_1 == "" || res.ISO639_2 == "" || res.Name == "" {
		t.Fatalf("expected non-empty detection result, got %+v", res)
	}
}
