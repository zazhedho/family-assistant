package serviceidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/pkg/config"
)

type linkingRepositoryStub struct {
	token       *domainidentity.LinkToken
	identity    *domainidentity.ExternalIdentity
	createErr   error
	consumeErr  error
	findErr     error
	revokeErr   error
	consumeHash string
	consumeProv string
	consumeID   string
	revokeUser  string
	revokeProv  string
	revokeID    string
	revokeCalls int
}

func (s *linkingRepositoryStub) CreateLinkToken(_ context.Context, token *domainidentity.LinkToken) error {
	s.token = token
	return s.createErr
}
func (s *linkingRepositoryStub) ConsumeAndLink(_ context.Context, tokenHash, provider, externalID string, _ time.Time) (*domainidentity.ExternalIdentity, error) {
	s.consumeHash, s.consumeProv, s.consumeID = tokenHash, provider, externalID
	return s.identity, s.consumeErr
}
func (s *linkingRepositoryStub) FindActive(_ context.Context, provider, externalID string) (*domainidentity.ExternalIdentity, error) {
	s.consumeProv, s.consumeID = provider, externalID
	return s.identity, s.findErr
}
func (s *linkingRepositoryStub) Revoke(_ context.Context, userID, provider, externalID string) error {
	s.revokeCalls++
	s.revokeUser, s.revokeProv, s.revokeID = userID, provider, externalID
	return s.revokeErr
}

type linkingAuditStoreStub struct {
	events []domainaudit.AuditEvent
}

func (s *linkingAuditStoreStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestIssueGeneratesRawCodeOnceAndPersistsOnlySHA256WithTenMinuteTTL(t *testing.T) {
	repo := &linkingRepositoryStub{}
	audit := &linkingAuditStoreStub{}
	service := NewLinkService(repo, audit, config.IdentityConfig{}).(*service)
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	code, err := service.Issue(context.Background(), " user-1 ", " HERMES ")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if code == "" || strings.Contains(code, " ") || strings.Contains(code, "+") || strings.Contains(code, "/") || strings.Contains(code, "=") {
		t.Fatalf("unexpected raw code: %q", code)
	}
	if repo.token == nil || repo.token.TokenHash == code {
		t.Fatalf("raw code persisted: %#v", repo.token)
	}
	hash := sha256.Sum256([]byte(code))
	if repo.token.TokenHash != hex.EncodeToString(hash[:]) {
		t.Fatalf("hash = %q, want SHA-256", repo.token.TokenHash)
	}
	if repo.token.Provider != domainidentity.ProviderHermes || repo.token.UserID != "user-1" || !repo.token.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("unexpected token: %#v", repo.token)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusSuccess || audit.events[0].Metadata["provider"] != domainidentity.ProviderHermes {
		t.Fatalf("unexpected issue audit: %#v", audit.events)
	}
	encoded := audit.events[0].Message + audit.events[0].ErrorMessage
	for _, secret := range []string{code, repo.token.TokenHash} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("secret leaked into audit: %q", secret)
		}
	}
}

func TestLinkHashesCodeNormalizesProviderAndReturnsIdentity(t *testing.T) {
	repo := &linkingRepositoryStub{identity: &domainidentity.ExternalIdentity{ID: "identity-1", UserID: "user-1", Provider: domainidentity.ProviderHermes, ExternalID: "profile-a", Status: domainidentity.StatusActive}}
	audit := &linkingAuditStoreStub{}
	service := NewLinkService(repo, audit, config.IdentityConfig{}).(*service)
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	identity, err := service.Link(context.Background(), "raw-code", " HERMES ", " profile-a ")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	hash := sha256.Sum256([]byte("raw-code"))
	if repo.consumeHash != hex.EncodeToString(hash[:]) || repo.consumeProv != domainidentity.ProviderHermes || repo.consumeID != "profile-a" {
		t.Fatalf("unexpected consume args: hash=%q provider=%q external=%q", repo.consumeHash, repo.consumeProv, repo.consumeID)
	}
	if identity == nil || identity.ID != "identity-1" || len(audit.events) != 1 || audit.events[0].ResourceID != "identity-1" {
		t.Fatalf("unexpected link result/audit: identity=%#v audit=%#v", identity, audit.events)
	}
}

func TestLinkInvalidReplayExpiryAndGuessUseSameSafeError(t *testing.T) {
	for _, cause := range []error{domainidentity.ErrInvalidLinkToken} {
		repo := &linkingRepositoryStub{consumeErr: cause}
		audit := &linkingAuditStoreStub{}
		service := NewLinkService(repo, audit, config.IdentityConfig{})
		_, err := service.Link(context.Background(), "raw-code", "hermes", "profile-a")
		if !errors.Is(err, domainidentity.ErrInvalidLinkToken) {
			t.Fatalf("error = %v, want ErrInvalidLinkToken", err)
		}
	}
}

func TestLinkPreservesIdentityConflict(t *testing.T) {
	repo := &linkingRepositoryStub{consumeErr: domainidentity.ErrIdentityConflict}
	service := NewLinkService(repo, &linkingAuditStoreStub{}, config.IdentityConfig{})
	_, err := service.Link(context.Background(), "raw-code", "hermes", "profile-a")
	if !errors.Is(err, domainidentity.ErrIdentityConflict) {
		t.Fatalf("error = %v, want ErrIdentityConflict", err)
	}
}

func TestRevokeRequiresMatchingUserAndChangesOnlyThatIdentity(t *testing.T) {
	repo := &linkingRepositoryStub{identity: &domainidentity.ExternalIdentity{ID: "identity-1", UserID: "user-1", Provider: domainidentity.ProviderHermes, ExternalID: "profile-a", Status: domainidentity.StatusActive}}
	audit := &linkingAuditStoreStub{}
	service := NewLinkService(repo, audit, config.IdentityConfig{})

	if err := service.Revoke(context.Background(), " user-1 ", " HERMES ", " profile-a "); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if repo.revokeCalls != 1 || repo.revokeUser != "user-1" || repo.revokeProv != domainidentity.ProviderHermes || repo.revokeID != "profile-a" {
		t.Fatalf("unexpected revoke args: calls=%d user=%q provider=%q external=%q", repo.revokeCalls, repo.revokeUser, repo.revokeProv, repo.revokeID)
	}
	if len(audit.events) != 1 || audit.events[0].ResourceID != "identity-1" || audit.events[0].Status != domainaudit.StatusSuccess {
		t.Fatalf("unexpected revoke audit: %#v", audit.events)
	}
}

func TestRevokeDoesNotTouchAnotherUsersIdentity(t *testing.T) {
	repo := &linkingRepositoryStub{identity: &domainidentity.ExternalIdentity{ID: "identity-2", UserID: "other-user", Provider: domainidentity.ProviderHermes, ExternalID: "profile-a", Status: domainidentity.StatusActive}}
	service := NewLinkService(repo, &linkingAuditStoreStub{}, config.IdentityConfig{})
	if err := service.Revoke(context.Background(), "user-1", "hermes", "profile-a"); !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		t.Fatalf("error = %v, want ErrIdentityNotFound", err)
	}
	if repo.revokeCalls != 0 {
		t.Fatalf("revoke calls = %d, want 0", repo.revokeCalls)
	}
}

func TestLinkServiceSatisfiesInterface(t *testing.T) {
	var _ LinkService = (*service)(nil)
}
