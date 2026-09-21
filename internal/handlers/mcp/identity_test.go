package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainidentity "family-assistant/internal/domain/identity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
)

type identityLinkServiceStub struct {
	identity   *domainidentity.ExternalIdentity
	err        error
	rawCode    string
	provider   string
	externalID string
	calls      int
}

func (s *identityLinkServiceStub) Issue(context.Context, string, string) (string, error) {
	return "", errors.New("not implemented")
}

func (s *identityLinkServiceStub) Link(_ context.Context, rawCode, provider, externalID string) (*domainidentity.ExternalIdentity, error) {
	s.calls++
	s.rawCode, s.provider, s.externalID = rawCode, provider, externalID
	return s.identity, s.err
}

func (s *identityLinkServiceStub) Revoke(context.Context, string, string, string) error {
	return errors.New("not implemented")
}

func TestIdentityLinkUsesTrustedExternalRequestWithoutLinkedActor(t *testing.T) {
	service := &identityLinkServiceStub{identity: &domainidentity.ExternalIdentity{
		ID: "identity-1", UserID: "user-1", Provider: domainidentity.ProviderHermes,
		ExternalID: "profile-new", Status: domainidentity.StatusActive,
	}}
	ctx := WithExternalRequest(context.Background(), ExternalRequest{
		Provider: domainidentity.ProviderHermes, ExternalID: "profile-new", Channel: "whatsapp",
	})

	got, err := IdentityLink(ctx, service, IdentityLinkInput{Code: "ABC123"})
	if err != nil {
		t.Fatalf("identity_link: %v", err)
	}
	if service.calls != 1 || service.rawCode != "ABC123" || service.provider != domainidentity.ProviderHermes || service.externalID != "profile-new" {
		t.Fatalf("unexpected link call: %+v", service)
	}
	if got.ID != "identity-1" || got.UserID != "user-1" || got.Provider != domainidentity.ProviderHermes || got.ExternalID != "profile-new" {
		t.Fatalf("unexpected safe identity output: %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	if strings.Contains(string(encoded), "ABC123") {
		t.Fatalf("link code leaked in output: %s", encoded)
	}
}

func TestIdentityLinkRequiresTrustedRequestAndValidCode(t *testing.T) {
	service := &identityLinkServiceStub{identity: &domainidentity.ExternalIdentity{ID: "identity-1"}}
	for _, tt := range []struct {
		name  string
		ctx   context.Context
		input IdentityLinkInput
		want  string
	}{
		{name: "missing trusted request", ctx: context.Background(), input: IdentityLinkInput{Code: "ABC123"}, want: "unauthenticated"},
		{name: "missing code", ctx: WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes", ExternalID: "profile-new"}), input: IdentityLinkInput{}, want: "invalid_input"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := IdentityLink(tt.ctx, service, tt.input)
			var mapped *MCPError
			if !errors.As(err, &mapped) || mapped.Code != tt.want {
				t.Fatalf("error = %T %v, want MCPError code %q", err, err, tt.want)
			}
		})
	}
	if service.calls != 0 {
		t.Fatalf("invalid identity_link input reached service %d times", service.calls)
	}
}

func TestIdentityLinkMapsTokenErrorsWithoutLeakingCode(t *testing.T) {
	service := &identityLinkServiceStub{err: domainidentity.ErrInvalidLinkToken}
	ctx := WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes", ExternalID: "profile-new"})

	_, err := IdentityLink(ctx, service, IdentityLinkInput{Code: "secret-code"})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "invalid_input" || mapped.Message != "invalid input" {
		t.Fatalf("mapped error = %T %v, want safe invalid input", err, err)
	}
	if strings.Contains(err.Error(), "secret-code") {
		t.Fatalf("link code leaked in error: %v", err)
	}
}

var _ interfaceidentity.LinkService = (*identityLinkServiceStub)(nil)
