package mcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainfamilymember "family-assistant/internal/domain/familymember"
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
	actor       serviceidentity.ActorContext
	err         error
	profileID   string
	channel     string
	resolveCall int
}

type legacyFamilyResolverRepoStub struct {
	member *domainfamilymember.ResolvedMember
}

func (s *legacyFamilyResolverRepoStub) FindActiveByHermesProfile(context.Context, string) (*domainfamilymember.ResolvedMember, error) {
	return s.member, nil
}

func (s *legacyFamilyResolverRepoStub) FindActiveByUserID(context.Context, string) (*domainfamilymember.ResolvedMember, error) {
	return s.member, nil
}

func (s *legacyFamilyResolverRepoStub) FindActiveByID(context.Context, string, string) (*domainfamilymember.FamilyMember, error) {
	return nil, errors.New("not implemented")
}

type legacyPermissionResolverStub struct{}

func (*legacyPermissionResolverStub) GetRolePermissions(context.Context, string) ([]domainpermission.Permission, error) {
	return []domainpermission.Permission{{Resource: "reminders", Action: "view"}}, nil
}

func (s *resolverStub) Resolve(_ context.Context, profileID, channel string) (serviceidentity.ActorContext, error) {
	s.resolveCall++
	s.profileID = profileID
	s.channel = channel
	return s.actor, s.err
}

func TestMCPCompatibilityDispatchUsesLegacyResolverServiceMode(t *testing.T) {
	resolver := serviceidentity.NewResolver(
		&legacyFamilyResolverRepoStub{member: &domainfamilymember.ResolvedMember{
			UserID: "user-1", MemberID: "member-1", FamilyID: "family-1", RoleID: "role-1", RoleName: "parent", HermesProfileID: "profile-1",
		}},
		&legacyPermissionResolverStub{},
	)

	actor, err := resolveActor(context.Background(), resolver, "profile-1", "whatsapp")
	if err != nil {
		t.Fatalf("legacy resolver dispatch: %v", err)
	}
	if actor.UserID != "user-1" || actor.FamilyID != "family-1" || actor.HermesProfileID != "profile-1" || !actor.HasPermission("reminders:view") {
		t.Fatalf("unexpected legacy actor: %+v", actor)
	}
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
		{name: "missing profile", header: "Bearer secret", setup: func(*http.Request) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &resolverStub{err: serviceidentity.ErrUnauthenticated}
			middleware := NewAuthMiddleware(config.MCPConfig{
				ServerKey:     "secret",
				ProfileHeader: "X-Hermes-Profile",
			}, resolver)
			reached := false
			handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				reached = true
			}))

			req := httptest.NewRequest(http.MethodPost, "/mcp?member_id=caller-controlled&family_id=other&role=admin", strings.NewReader(`{"member_id":"spoofed","family_id":"other","role":"admin"}`))
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

func TestMCPAuthMiddlewareUsesConfiguredProfileHeaderAndAttachesTrustedExternalRequest(t *testing.T) {
	resolver := &resolverStub{actor: serviceidentity.ActorContext{
		UserID:   "user-1",
		MemberID: "member-1",
		FamilyID: "family-1",
		Source:   "mcp",
		RoleName: "parent",
	}}
	middleware := NewAuthMiddleware(config.MCPConfig{
		ServerKey:     "secret",
		ProfileHeader: "X-Trusted-Profile",
	}, resolver)

	var got ExternalRequest
	handler := middleware.Handler(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		var ok bool
		got, ok = ExternalRequestFromContext(req.Context())
		if !ok {
			t.Error("expected trusted external request context")
		}
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp?member_id=spoofed&family_id=other&role=admin", strings.NewReader(`{"member_id":"spoofed","family_id":"other","role":"admin"}`))
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
	}, resolver)

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
	resolver := &resolverStub{actor: serviceidentity.ActorContext{UserID: "user-1"}}
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
