package mcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
	"github.com/zazhedho/family-assistant/pkg/config"
)

func TestSecretDigestUsesFixedSizeHashForComparison(t *testing.T) {
	digest := secretDigest("short")
	if len(digest) != sha256.Size {
		t.Fatalf("expected fixed SHA-256 digest size %d, got %d", sha256.Size, len(digest))
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

func (s *resolverStub) Resolve(_ context.Context, profileID, channel string) (serviceidentity.ActorContext, error) {
	s.resolveCall++
	s.profileID = profileID
	s.channel = channel
	return s.actor, s.err
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
		{
			name:   "unknown profile",
			header: "Bearer secret",
			setup: func(req *http.Request) {
				req.Header.Set("X-Hermes-Profile", "unknown")
			},
		},
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
			if resolver.resolveCall > 0 && tt.name != "unknown profile" {
				t.Fatalf("expected invalid credentials/profile to skip resolver, calls=%d", resolver.resolveCall)
			}
		})
	}
}

func TestMCPAuthMiddlewareUsesConfiguredProfileHeaderAndAttachesActor(t *testing.T) {
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

	var got serviceidentity.ActorContext
	handler := middleware.Handler(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		var ok bool
		got, ok = ActorFromContext(req.Context())
		if !ok {
			t.Error("expected actor context")
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
	if resolver.profileID != "profile-parent" || resolver.channel != "whatsapp" {
		t.Fatalf("resolver received untrusted identity metadata: profile=%q channel=%q", resolver.profileID, resolver.channel)
	}
	if got.UserID != "user-1" || got.MemberID != "member-1" || got.FamilyID != "family-1" || got.RoleName != "parent" {
		t.Fatalf("unexpected actor: %+v", got)
	}
}

func TestMCPAuthMiddlewarePropagatesResolverFailureSafely(t *testing.T) {
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

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for resolver failure, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "database details") {
		t.Fatalf("resolver details leaked to client: %s", rec.Body.String())
	}
}
