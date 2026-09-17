package configvalue

import (
	"testing"
	"time"
)

func TestBoolSupportsCommonFeatureFlagValues(t *testing.T) {
	cases := map[string]bool{
		"true":     true,
		"enabled":  true,
		"on":       true,
		"1":        true,
		"false":    false,
		"disabled": false,
		"off":      false,
		"0":        false,
	}

	for input, expected := range cases {
		actual, err := Bool(input, false)
		if err != nil {
			t.Fatalf("expected success for %q, got %v", input, err)
		}
		if actual != expected {
			t.Fatalf("expected %v for %q, got %v", expected, input, actual)
		}
	}
}

func TestStringAndInvalidValues(t *testing.T) {
	if got := String(" value ", "fallback"); got != "value" {
		t.Fatalf("expected trimmed string, got %q", got)
	}
	if got := String("   ", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback string, got %q", got)
	}
	if got, err := Bool("maybe", true); err == nil || !got {
		t.Fatalf("expected bool fallback with error, got %v err=%v", got, err)
	}
	if got, err := Int("abc", 7); err == nil || got != 7 {
		t.Fatalf("expected int fallback with error, got %v err=%v", got, err)
	}
	if got, err := Duration("abc", time.Second); err == nil || got != time.Second {
		t.Fatalf("expected duration fallback with error, got %v err=%v", got, err)
	}
	if err := JSON("", nil); err != nil {
		t.Fatalf("expected empty JSON to be ignored, got %v", err)
	}
}

func TestIntReturnsFallbackWhenEmpty(t *testing.T) {
	actual, err := Int("", 42)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if actual != 42 {
		t.Fatalf("expected fallback 42, got %d", actual)
	}
}

func TestDurationParsesValidValue(t *testing.T) {
	actual, err := Duration("15m", time.Minute)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if actual != 15*time.Minute {
		t.Fatalf("expected 15m, got %v", actual)
	}
}

func TestJSONDecodesIntoTarget(t *testing.T) {
	type payload struct {
		Enabled bool `json:"enabled"`
		Limit   int  `json:"limit"`
	}

	var actual payload
	if err := JSON(`{"enabled":true,"limit":5}`, &actual); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !actual.Enabled || actual.Limit != 5 {
		t.Fatalf("unexpected payload: %+v", actual)
	}
}
