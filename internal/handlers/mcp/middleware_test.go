package mcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/config"
)

func TestSecretDigestUsesFixedSizeHashForComparison(t *testing.T) {
	if got := len(secretDigest("short")); got != sha256.Size {
		t.Fatalf("expected fixed SHA-256 digest size %d, got %d", sha256.Size, got)
	}
	if !constantTimeEqual("short", "short") {
		t.Fatal("expected equal secrets to compare equal")
	}
	if constantTimeEqual("short", "a much longer secret") {
		t.Fatal("expected different-length secrets to compare unequal")
	}
}

type resolverStub struct {
	actor       domainidentity.ActorContext
	err         error
	profileID   string
	channel     string
	resolveCall int
	permissions []domainpermission.Permission
}

func (s *resolverStub) Resolve(_ context.Context, profileID, channel string) (domainidentity.ActorContext, error) {
	s.resolveCall++
	s.profileID = profileID
	s.channel = channel
	return s.actor, s.err
}

func (s *resolverStub) ResolveExternal(ctx context.Context, _ string, profileID, channel string) (domainidentity.ActorContext, error) {
	return s.Resolve(ctx, profileID, channel)
}

func (s *resolverStub) GetRolePermissions(context.Context, string) ([]domainpermission.Permission, error) {
	return s.permissions, nil
}

func TestMCPAuthMiddlewareRejectsUnauthenticatedRequests(t *testing.T) {
	tests := []struct {
		name   string
		header string
		setup  func(*http.Request)
	}{
		{name: "missing bearer", setup: func(*http.Request) {}},
		{name: "wrong bearer", header: "Bearer wrong", setup: func(*http.Request) {}},
		{name: "wrong scheme", header: "Basic secret", setup: func(*http.Request) {}},
		{name: "extra bearer token", header: "Bearer secret extra", setup: func(*http.Request) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &resolverStub{err: serviceidentity.ErrUnauthenticated}
			middleware := NewAuthMiddleware(config.MCPConfig{
				ServerKey:     "secret",
				ProfileHeader: "X-Hermes-Profile",
			})
			reached := false
			handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				reached = true
			}))

			req := httptest.NewRequest(http.MethodPost, "/mcp?spoofed=caller-controlled&role=admin", strings.NewReader(`{"spoofed":"attacker","role":"admin"}`))
			req.Header.Set("Authorization", tt.header)
			tt.setup(req)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
			}
			if reached {
				t.Fatal("expected unauthenticated request to stop before next handler")
			}
			if resolver.resolveCall > 0 {
				t.Fatalf("expected invalid credentials/profile to skip resolver, calls=%d", resolver.resolveCall)
			}
		})
	}
}

func TestMCPAuthMiddlewareAllowsServerAuthenticatedRequestsWithoutProfile(t *testing.T) {
	middleware := NewAuthMiddleware(config.MCPConfig{
		ServerKey:     "secret",
		ProfileHeader: "X-Hermes-Profile",
	})

	var got ExternalRequest
	handler := middleware.Handler(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		var ok bool
		got, ok = ExternalRequestFromContext(req.Context())
		if !ok {
			t.Error("expected trusted external request context")
		}
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("server-authenticated request without profile status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got.Provider != domainidentity.ProviderHermes || got.ExternalID != "" || got.Channel != "whatsapp" {
		t.Fatalf("unexpected request context: %+v", got)
	}
}

func TestMCPAuthMiddlewareUsesConfiguredProfileHeaderAndAttachesTrustedExternalRequest(t *testing.T) {
	resolver := &resolverStub{actor: domainidentity.ActorContext{
		UserID:   "user-1",
		MemberID: "member-1",
		Source:   "mcp",
		RoleName: "parent",
	}}
	middleware := NewAuthMiddleware(config.MCPConfig{
		ServerKey:     "secret",
		ProfileHeader: "X-Trusted-Profile",
	})

	var got ExternalRequest
	handler := middleware.Handler(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		var ok bool
		got, ok = ExternalRequestFromContext(req.Context())
		if !ok {
			t.Error("expected trusted external request context")
		}
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp?spoofed=caller-controlled&role=admin", strings.NewReader(`{"spoofed":"attacker","role":"admin"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Trusted-Profile", "profile-parent")
	req.Header.Set("X-Hermes-Profile", "attacker-profile")
	// Missing channel must use the protocol's WhatsApp default.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected valid request to pass, got %d: %s", rec.Code, rec.Body.String())
	}
	if resolver.resolveCall != 0 {
		t.Fatalf("outer middleware resolved actor %d times", resolver.resolveCall)
	}
	if got.Provider != "hermes" || got.ExternalID != "profile-parent" || got.Channel != "whatsapp" {
		t.Fatalf("unexpected trusted request: %+v", got)
	}
}

func TestMCPAuthMiddlewareDoesNotResolveActor(t *testing.T) {
	resolver := &resolverStub{err: errors.New("database details must not reach client")}
	middleware := NewAuthMiddleware(config.MCPConfig{
		ServerKey:     "secret",
		ProfileHeader: "X-Hermes-Profile",
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Hermes-Profile", "profile-parent")
	rec := httptest.NewRecorder()
	middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected server-authenticated request to pass, got %d", rec.Code)
	}
	if resolver.resolveCall != 0 {
		t.Fatalf("outer middleware resolved actor %d times", resolver.resolveCall)
	}
}

func TestRequireActorUsesOnlyTrustedExternalRequest(t *testing.T) {
	resolver := &resolverStub{actor: domainidentity.ActorContext{UserID: "user-1"}}
	ctx := WithExternalRequest(context.Background(), ExternalRequest{
		Provider: "hermes", ExternalID: "profile-parent", Channel: "whatsapp",
	})

	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		t.Fatalf("require actor: %v", err)
	}
	if actor.UserID != "user-1" || resolver.profileID != "profile-parent" || resolver.channel != "whatsapp" {
		t.Fatalf("unexpected actor resolution: actor=%+v profile=%q channel=%q", actor, resolver.profileID, resolver.channel)
	}

	if _, err := RequireActor(context.Background(), resolver); !errors.Is(err, serviceidentity.ErrUnauthenticated) {
		t.Fatalf("missing trusted request error = %v, want unauthenticated", err)
	}
}
