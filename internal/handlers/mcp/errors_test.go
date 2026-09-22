package mcp

import (
	"errors"
	"fmt"
	"testing"

	domaininvitation "family-assistant/internal/domain/invitation"
	servicereminder "family-assistant/internal/services/reminder"
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

func TestMapToolErrorMapsInvitationErrorsSafely(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		code string
	}{
		{name: "invalid", err: domaininvitation.ErrInvalidInvitation, code: "invalid_input"},
		{name: "membership conflict", err: domaininvitation.ErrMembershipConflict, code: "conflict"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mapped *MCPError
			if err := MapToolError(tt.err); !errors.As(err, &mapped) || mapped.Code != tt.code {
				t.Fatalf("mapped error = %#v, want code %q", mapped, tt.code)
			}
		})
	}
}
