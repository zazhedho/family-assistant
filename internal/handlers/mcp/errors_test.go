package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	domaininvitation "family-assistant/internal/domain/invitation"
	serviceauthorization "family-assistant/internal/services/authorization"
	servicereminder "family-assistant/internal/services/reminder"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
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

func TestMapToolErrorMapsPhoneAlreadyRegisteredSafely(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	var mapped *MCPError
	err := MapToolError(fmt.Errorf("register: %w", domainidentity.ErrPhoneAlreadyRegistered), logger)
	if !errors.As(err, &mapped) {
		t.Fatalf("mapped error = %T %v, want MCPError", err, err)
	}
	if mapped.Code != "phone_already_registered" || mapped.Message != ErrMCPPhoneAlreadyRegistered.Error() {
		t.Fatalf("mapped = %#v, want phone_already_registered with safe message", mapped)
	}
	if !strings.Contains(mapped.Message, "admin") {
		t.Fatalf("message = %q, want guidance to ask the admin", mapped.Message)
	}
	for _, forbidden := range []string{"identity_link", "web", "one-time"} {
		if strings.Contains(mapped.Message, forbidden) {
			t.Fatalf("message = %q, must not mention %q", mapped.Message, forbidden)
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("unexpected log output: %s", buf.String())
	}
}

func TestMapToolErrorExplainsValidationErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{name: "validation", err: fmt.Errorf("report: %w", &serviceauthorization.ValidationError{Field: "details.leftover_ml", Reason: "must not exceed volume_ml"}), want: "invalid input: details.leftover_ml must not exceed volume_ml"},
		{name: "validation without field", err: &serviceauthorization.ValidationError{Reason: "is required"}, want: "invalid input: is required"},
		{name: "details", err: &domainactivity.DetailsError{Field: "expired_ml", Reason: "is required when outcome is expired"}, want: "invalid input: expired_ml is required when outcome is expired"},
		{name: "check violation", err: fmt.Errorf("update: %w", &pgconn.PgError{Code: "23514", Message: "violates check constraint \"ck_space_activities_details_size\""}), want: "invalid input: details violate a constraint"},
		{name: "translated check violation", err: fmt.Errorf("%w: %w", gorm.ErrCheckConstraintViolated, errors.New("raw driver text")), want: "invalid input: details violate a constraint"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mapped *MCPError
			if err := MapToolError(tt.err); !errors.As(err, &mapped) || mapped.Code != "invalid_input" || mapped.Message != tt.want {
				t.Fatalf("mapped = %#v, want invalid_input %q", mapped, tt.want)
			}
		})
	}
}
