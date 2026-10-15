package mcp

import (
	"errors"
	"testing"

	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	serviceidentity "family-assistant/internal/services/identity"
)

func TestMapToolErrorMapsScheduledMessageConflict(t *testing.T) {
	got := MapToolError(interfacescheduledmessage.ErrConflict)
	var mapped *MCPError
	if !errors.As(got, &mapped) || mapped.Code != "conflict" || mapped.Message != ErrMCPConflict.Error() {
		t.Fatalf("mapped error = %#v, want scheduled message conflict", got)
	}
}

func TestMapToolErrorMapsInvalidScheduledMessageIdentity(t *testing.T) {
	got := MapToolError(interfacescheduledmessage.ErrInvalidWhatsAppIdentity)
	var mapped *MCPError
	if !errors.As(got, &mapped) || mapped.Code != "invalid_input" || mapped.Message != ErrMCPInvalidInput.Error() {
		t.Fatalf("mapped error = %#v, want invalid scheduled message identity", got)
	}
}

func TestMapToolErrorMapsUnknownSelectedSpaceAsNotFound(t *testing.T) {
	got := MapToolError(serviceidentity.ErrNotFound)
	var mapped *MCPError
	if !errors.As(got, &mapped) || mapped.Code != "not_found" || mapped.Message != ErrMCPNotFound.Error() {
		t.Fatalf("mapped error = %#v, want not found", got)
	}
}
