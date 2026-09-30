package domainreminder

import (
	"errors"
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
