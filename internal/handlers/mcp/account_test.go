package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	serviceidentity "family-assistant/internal/services/identity"
	serviceonboarding "family-assistant/internal/services/onboarding"
)

type registrarStub struct {
	input  serviceonboarding.Input
	result serviceonboarding.Result
	err    error
	calls  int
}

func (s *registrarStub) Register(_ context.Context, input serviceonboarding.Input) (serviceonboarding.Result, error) {
	s.calls++
	s.input = input
	return s.result, s.err
}

func TestAccountRegisterUsesOnlyTrustedExternalRequest(t *testing.T) {
	service := &registrarStub{result: serviceonboarding.Result{Status: "created", UserID: "user-1", SpaceID: "space-1"}}
	ctx := WithExternalRequest(context.Background(), ExternalRequest{
		Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp",
	})

	got, err := AccountRegister(ctx, service, AccountRegisterInput{
		Name: "Jane Doe", BirthDate: "1990-05-20", Consent: true,
	})
	if err != nil || got != (AccountRegisterOutput{Status: "created", UserID: "user-1", SpaceID: "space-1"}) {
		t.Fatalf("output = %+v, err = %v", got, err)
	}
	if service.input.Provider != "hermes" || service.input.ExternalID != "profile-1" || service.input.Channel != "whatsapp" || service.input.Name != "Jane Doe" || service.input.BirthDate != "1990-05-20" || !service.input.Consent {
		t.Fatalf("service input = %+v", service.input)
	}
	encoded, err := json.Marshal(got)
	if err != nil || strings.Contains(string(encoded), "1990-05-20") || strings.Contains(string(encoded), "profile-1") {
		t.Fatalf("unsafe output = %s, err = %v", encoded, err)
	}
}

func TestAccountRegisterRequiresTrustedExternalRequest(t *testing.T) {
	for _, tt := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "missing trusted request", ctx: context.Background()},
		{name: "empty provider", ctx: WithExternalRequest(context.Background(), ExternalRequest{ExternalID: "profile-1"})},
		{name: "empty external id", ctx: WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes"})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &registrarStub{}
			_, err := AccountRegister(tt.ctx, service, AccountRegisterInput{})
			var mapped *MCPError
			if !errors.As(err, &mapped) || mapped.Code != "unauthenticated" {
				t.Fatalf("error = %T %v, want unauthenticated", err, err)
			}
			if service.calls != 0 {
				t.Fatalf("registrar calls = %d, want 0", service.calls)
			}
		})
	}
}

func TestAccountRegisterMapsServiceErrorsSafely(t *testing.T) {
	ctx := WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp"})
	for _, tt := range []struct {
		name       string
		registrar  serviceonboarding.Registrar
		input      AccountRegisterInput
		serviceErr error
		wantCode   string
		wantMsg    string
	}{
		{
			name:     "nil registrar",
			input:    AccountRegisterInput{Name: "Jane Doe", BirthDate: "1990-05-20", Consent: true},
			wantCode: "internal",
			wantMsg:  "internal server error",
		},
		{
			name:       "consent validation",
			input:      AccountRegisterInput{Name: "Jane Doe", BirthDate: "1990-05-20"},
			serviceErr: &serviceidentity.ValidationError{Field: "consent", Reason: "is required"},
			wantCode:   "invalid_input",
			wantMsg:    "invalid input",
		},
		{
			name:       "underage validation",
			input:      AccountRegisterInput{Name: "Jane Doe", BirthDate: "2010-09-21", Consent: true},
			serviceErr: &serviceidentity.ValidationError{Field: "birth_date", Reason: "must be at least 18 years old"},
			wantCode:   "invalid_input",
			wantMsg:    "invalid input",
		},
		{
			name:       "persistence failure",
			input:      AccountRegisterInput{Name: "Jane Doe", BirthDate: "1990-05-20", Consent: true},
			serviceErr: errors.New("insert failed"),
			wantCode:   "internal",
			wantMsg:    "internal server error",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var registrar serviceonboarding.Registrar
			if tt.name != "nil registrar" {
				registrar = &registrarStub{err: tt.serviceErr}
			}
			_, err := AccountRegister(ctx, registrar, tt.input)
			var mapped *MCPError
			if !errors.As(err, &mapped) || mapped.Code != tt.wantCode || mapped.Message != tt.wantMsg {
				t.Fatalf("error = %T %v, want %s/%s", err, err, tt.wantCode, tt.wantMsg)
			}
			if strings.Contains(err.Error(), "2010-09-21") || strings.Contains(err.Error(), "profile-1") {
				t.Fatalf("private value leaked in error: %v", err)
			}
		})
	}
}

var _ serviceonboarding.Registrar = (*registrarStub)(nil)
