package serviceidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
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
func (s *linkingRepositoryStub) FindActiveByUserID(_ context.Context, _, _ string) (*domainidentity.ExternalIdentity, error) {
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

func TestIssueCodesUse32BytesAndAreDistinct(t *testing.T) {
	repo := &linkingRepositoryStub{}
	service := NewLinkService(repo, &linkingAuditStoreStub{}, config.IdentityConfig{}).(*service)
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	first, err := service.Issue(context.Background(), "user-1", domainidentity.ProviderHermes)
	if err != nil {
		t.Fatalf("issue first code: %v", err)
	}
	firstHash := repo.token.TokenHash
	second, err := service.Issue(context.Background(), "user-1", domainidentity.ProviderHermes)
	if err != nil {
		t.Fatalf("issue second code: %v", err)
	}
	secondHash := repo.token.TokenHash
	if len(first) != 43 || len(second) != 43 {
		t.Fatalf("code lengths = %d, %d; want 43", len(first), len(second))
	}
	if first == second || firstHash == secondHash {
		t.Fatalf("successive codes/hashes must differ: %q/%q", first, second)
	}
	if len(firstHash) != sha256.Size*2 || len(secondHash) != sha256.Size*2 {
		t.Fatalf("hash lengths = %d, %d; want %d", len(firstHash), len(secondHash), sha256.Size*2)
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

func TestLinkInvalidReplayExpiryGuessMalformedAndWrongProviderUseSameSafeError(t *testing.T) {
	tests := []struct {
		name     string
		rawCode  string
		provider string
		repoErr  error
	}{
		{name: "replayed", rawCode: "replayed", provider: domainidentity.ProviderHermes, repoErr: domainidentity.ErrInvalidLinkToken},
		{name: "expired", rawCode: "expired", provider: domainidentity.ProviderHermes, repoErr: domainidentity.ErrInvalidLinkToken},
		{name: "guessed", rawCode: "guessed", provider: domainidentity.ProviderHermes, repoErr: domainidentity.ErrInvalidLinkToken},
		{name: "malformed", rawCode: " ", provider: domainidentity.ProviderHermes, repoErr: domainidentity.ErrInvalidLinkToken},
		{name: "wrong provider", rawCode: "wrong-provider", provider: "other", repoErr: domainidentity.ErrInvalidLinkToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &linkingRepositoryStub{consumeErr: tt.repoErr}
			audit := &linkingAuditStoreStub{}
			service := NewLinkService(repo, audit, config.IdentityConfig{})
			ctx := auditContext("mcp")
			_, err := service.Link(ctx, tt.rawCode, tt.provider, "profile-a")
			if !errors.Is(err, domainidentity.ErrInvalidLinkToken) || FailureCategory(err) != "validation" {
				t.Fatalf("error/category = %v/%q, want invalid-link-token/validation", err, FailureCategory(err))
			}
			if len(audit.events) != 1 {
				t.Fatalf("audit events = %#v", audit.events)
			}
			event := audit.events[0]
			if event.Status != domainaudit.StatusFailed || event.Resource != "external_identity" || event.ErrorMessage != "validation" || event.Source != "mcp" {
				t.Fatalf("unexpected safe failure audit: %#v", event)
			}
		})
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

func TestIdentityAuditSuccessAndFailureKeepFullProvenanceAndNoSecrets(t *testing.T) {
	const rawCode = "raw-link-code"
	hash := sha256.Sum256([]byte(rawCode))
	secretMetadata := map[string]any{
		"link_code":    rawCode,
		"token_hash":   hex.EncodeToString(hash[:]),
		"bearer":       "bearer-secret",
		"request_body": map[string]any{"external_id": "profile-a"},
	}

	t.Run("issue success", func(t *testing.T) {
		repo := &linkingRepositoryStub{}
		audit := &linkingAuditStoreStub{}
		service := NewLinkService(repo, audit, config.IdentityConfig{})
		code, err := service.Issue(auditContextWithMetadata("http", secretMetadata), "subject-user", domainidentity.ProviderHermes)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		if len(audit.events) != 1 {
			t.Fatalf("audit events = %#v", audit.events)
		}
		event := audit.events[0]
		assertAuditProvenance(t, event, domainaudit.StatusSuccess, "identity_link_token", repo.token.ID, "http", "actor-user", "subject-user", domainidentity.ProviderHermes)
		assertNoSecrets(t, event, code, repo.token.TokenHash, "bearer-secret")
	})

	t.Run("issue failure", func(t *testing.T) {
		repo := &linkingRepositoryStub{createErr: errors.New("database down")}
		audit := &linkingAuditStoreStub{}
		service := NewLinkService(repo, audit, config.IdentityConfig{})
		if _, err := service.Issue(auditContextWithMetadata("http", secretMetadata), "subject-user", domainidentity.ProviderHermes); err == nil {
			t.Fatal("expected issue failure")
		}
		event := audit.events[0]
		assertAuditProvenance(t, event, domainaudit.StatusFailed, "identity_link_token", "", "http", "actor-user", "subject-user", domainidentity.ProviderHermes)
		assertNoSecrets(t, event, repo.token.TokenHash, "bearer-secret")
	})

	t.Run("link success", func(t *testing.T) {
		repo := &linkingRepositoryStub{identity: &domainidentity.ExternalIdentity{ID: "identity-1", UserID: "subject-user", Provider: domainidentity.ProviderHermes, ExternalID: "profile-a", Status: domainidentity.StatusActive}}
		audit := &linkingAuditStoreStub{}
		service := NewLinkService(repo, audit, config.IdentityConfig{})
		if _, err := service.Link(auditContextWithMetadata("mcp", secretMetadata), rawCode, domainidentity.ProviderHermes, "profile-a"); err != nil {
			t.Fatalf("link: %v", err)
		}
		event := audit.events[0]
		assertAuditProvenance(t, event, domainaudit.StatusSuccess, "external_identity", "identity-1", "mcp", "actor-user", "subject-user", domainidentity.ProviderHermes)
		assertNoSecrets(t, event, rawCode, hex.EncodeToString(hash[:]), "bearer-secret")
	})

	t.Run("link failure", func(t *testing.T) {
		repo := &linkingRepositoryStub{consumeErr: domainidentity.ErrInvalidLinkToken}
		audit := &linkingAuditStoreStub{}
		service := NewLinkService(repo, audit, config.IdentityConfig{})
		if _, err := service.Link(auditContextWithMetadata("mcp", secretMetadata), rawCode, domainidentity.ProviderHermes, "profile-a"); !errors.Is(err, domainidentity.ErrInvalidLinkToken) {
			t.Fatalf("error = %v, want invalid token", err)
		}
		event := audit.events[0]
		assertAuditProvenance(t, event, domainaudit.StatusFailed, "external_identity", "", "mcp", "actor-user", "", domainidentity.ProviderHermes)
		assertNoSecrets(t, event, rawCode, hex.EncodeToString(hash[:]), "bearer-secret")
	})

	t.Run("revoke success", func(t *testing.T) {
		repo := &linkingRepositoryStub{identity: &domainidentity.ExternalIdentity{ID: "identity-1", UserID: "subject-user", Provider: domainidentity.ProviderHermes, ExternalID: "profile-a", Status: domainidentity.StatusActive}}
		audit := &linkingAuditStoreStub{}
		service := NewLinkService(repo, audit, config.IdentityConfig{})
		if err := service.Revoke(auditContextWithMetadata("http", secretMetadata), "subject-user", domainidentity.ProviderHermes, "profile-a"); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		event := audit.events[0]
		assertAuditProvenance(t, event, domainaudit.StatusSuccess, "external_identity", "identity-1", "http", "actor-user", "subject-user", domainidentity.ProviderHermes)
		assertNoSecrets(t, event, rawCode, hex.EncodeToString(hash[:]), "bearer-secret")
	})

	t.Run("revoke failure", func(t *testing.T) {
		repo := &linkingRepositoryStub{findErr: errors.New("database down")}
		audit := &linkingAuditStoreStub{}
		service := NewLinkService(repo, audit, config.IdentityConfig{})
		if err := service.Revoke(auditContextWithMetadata("http", secretMetadata), "subject-user", domainidentity.ProviderHermes, "profile-a"); err == nil {
			t.Fatal("expected revoke failure")
		}
		event := audit.events[0]
		assertAuditProvenance(t, event, domainaudit.StatusFailed, "external_identity", "", "http", "actor-user", "subject-user", domainidentity.ProviderHermes)
		assertNoSecrets(t, event, rawCode, hex.EncodeToString(hash[:]), "bearer-secret")
	})
}

func auditContext(source string) context.Context {
	return auditContextWithMetadata(source, nil)
}

func auditContextWithMetadata(source string, metadata map[string]any) context.Context {
	scope := authscope.NewFromClaims(map[string]any{
		"user_id":           "subject-user",
		"username":          "Subject",
		"role":              "member",
		"is_impersonated":   true,
		"original_user_id":  "actor-user",
		"original_username": "Actor",
		"original_role":     "admin",
	}, nil)
	ctx := authscope.WithContext(context.Background(), scope)
	return WithAuditProvenance(ctx, AuditProvenance{
		Source:    source,
		RequestID: "request-1",
		IPAddress: "192.0.2.1",
		UserAgent: "identity-agent",
		Metadata:  metadata,
	})
}

func assertAuditProvenance(t *testing.T, event domainaudit.AuditEvent, status, resource, resourceID, source, actorUserID, subjectUserID, provider string) {
	t.Helper()
	if event.Status != status || event.Resource != resource || event.ResourceID != resourceID || event.ActorUserID != actorUserID || event.Source != source || event.RequestID != "request-1" || event.IPAddress != "192.0.2.1" || event.UserAgent != "identity-agent" {
		t.Fatalf("unexpected audit provenance: %#v", event)
	}
	if event.Metadata["provider"] != provider || (subjectUserID != "" && event.Metadata["subject_user_id"] != subjectUserID) {
		t.Fatalf("unexpected audit metadata: %#v", event.Metadata)
	}
}

func assertNoSecrets(t *testing.T, event domainaudit.AuditEvent, secrets ...string) {
	t.Helper()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal audit: %v", err)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(string(encoded), secret) {
			t.Fatalf("secret leaked into audit: %q in %s", secret, encoded)
		}
	}
}

func TestLinkServiceSatisfiesInterface(t *testing.T) {
	var _ interfaceidentity.LinkService = (*service)(nil)
}
