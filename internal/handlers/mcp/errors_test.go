package mcp

import (
	"errors"
	"fmt"
	"testing"

	servicereminder "github.com/zazhedho/family-assistant/internal/services/reminder"
)

func TestMapToolErrorMapsReminderConflictSafely(t *testing.T) {
	var mapped *MCPError
	err := MapToolError(fmt.Errorf("complete reminder: %w", servicereminder.ErrConflict))
	if !errors.As(err, &mapped) {
		t.Fatalf("mapped error = %T %v, want MCPError", err, err)
	}
	if mapped.Code != "conflict" || mapped.Message != "conflict" {
		t.Fatalf("mapped conflict = %#v, want code/message conflict", mapped)
	}
}
