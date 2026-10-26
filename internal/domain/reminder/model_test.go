package domainreminder

import (
	"errors"
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	for _, expected := range []Status{StatusPending, StatusSent, StatusCompleted, StatusCancelled} {
		got, err := ParseStatus(" " + string(expected) + " ")
		if err != nil || got == nil || *got != expected {
			t.Fatalf("ParseStatus(%s) = %v, %v", expected, got, err)
		}
	}
	if got, err := ParseStatus(" "); got != nil || err != nil {
		t.Fatalf("blank status = %v, %v; want nil, nil", got, err)
	}
	if _, err := ParseStatus("unknown"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v", err)
	}
}

func TestNormalizeReplaceKey(t *testing.T) {
	got, err := NormalizeReplaceKey("  Diaper ")
	if err != nil || got == nil || *got != "diaper" {
		t.Fatalf("NormalizeReplaceKey trim/lower = %v, %v", got, err)
	}
	for _, blank := range []string{"", "   "} {
		if got, err := NormalizeReplaceKey(blank); got != nil || err != nil {
			t.Fatalf("NormalizeReplaceKey(%q) = %v, %v; want nil, nil", blank, got, err)
		}
	}
	max := strings.Repeat("é", MaxReplaceKeyLength)
	if got, err := NormalizeReplaceKey(max); err != nil || got == nil || *got != max {
		t.Fatalf("128-rune key = %v, %v", got, err)
	}
	if got, err := NormalizeReplaceKey(strings.Repeat("é", MaxReplaceKeyLength+1)); got != nil || !errors.Is(err, ErrInvalidReplaceKey) {
		t.Fatalf("129-rune key = %v, %v; want ErrInvalidReplaceKey", got, err)
	}
}
