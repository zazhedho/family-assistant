package serviceidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/pkg/config"
	"family-assistant/utils"
	"gorm.io/gorm"
)

var (
	ErrInvalidIdentityProvider = errors.New("invalid identity provider")
	ErrInvalidExternalIdentity = errors.New("invalid external identity")
	ErrInvalidLinkToken        = domainidentity.ErrInvalidLinkToken
	ErrIdentityConflict        = domainidentity.ErrIdentityConflict
	ErrIdentityNotFound        = domainidentity.ErrIdentityNotFound
)

type LinkService interface {
	Issue(context.Context, string, string) (string, error)
	Link(context.Context, string, string, string) (*domainidentity.ExternalIdentity, error)
	Revoke(context.Context, string, string, string) error
}

type auditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}

type AuditProvenance struct {
	Source    string
	RequestID string
	IPAddress string
	UserAgent string
	Metadata  map[string]any
}

type auditProvenanceKey struct{}

func WithAuditProvenance(ctx context.Context, provenance AuditProvenance) context.Context {
	return context.WithValue(ctx, auditProvenanceKey{}, provenance)
}

type service struct {
	repository domainidentity.Repository
	audit      auditStore
	config     config.IdentityConfig
	now        func() time.Time
}

func NewLinkService(repository domainidentity.Repository, audit auditStore, configs ...config.IdentityConfig) LinkService {
	cfg := config.LoadIdentityConfig()
	if len(configs) > 0 {
		cfg = configs[0]
	}
	if cfg.TTL <= 0 {
		cfg.TTL = config.DefaultIdentityLinkTTL
	}
	return &service{
		repository: repository,
		audit:      audit,
		config:     cfg,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func NewLinkingService(repository domainidentity.Repository, audit auditStore, configs ...config.IdentityConfig) LinkService {
	return NewLinkService(repository, audit, configs...)
}

func NewService(repository domainidentity.Repository, audit auditStore, configs ...config.IdentityConfig) LinkService {
	return NewLinkService(repository, audit, configs...)
}

func (s *service) Issue(ctx context.Context, userID, provider string) (string, error) {
	userID = strings.TrimSpace(userID)
	provider = domainidentity.NormalizeProvider(provider)
	if userID == "" {
		return s.issueFailure(ctx, provider, userID, ErrInvalidExternalIdentity)
	}
	if provider == "" {
		return s.issueFailure(ctx, provider, userID, ErrInvalidIdentityProvider)
	}
	if s.repository == nil {
		return s.issueFailure(ctx, provider, userID, errors.New("identity repository is not configured"))
	}

	rawCode, err := newLinkCode()
	if err != nil {
		return s.issueFailure(ctx, provider, userID, err)
	}
	hash := sha256.Sum256([]byte(rawCode))
	now := s.now().UTC()
	token := &domainidentity.LinkToken{
		ID:        utils.CreateUUID(),
		UserID:    userID,
		Provider:  provider,
		TokenHash: hex.EncodeToString(hash[:]),
		ExpiresAt: now.Add(s.config.TTL),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repository.CreateLinkToken(ctx, token); err != nil {
		return s.issueFailure(ctx, provider, userID, err, rawCode, token.TokenHash)
	}
	s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionCreate, "identity_link_token", token.ID, userID, provider, domainaudit.StatusSuccess, "Issued identity link code", "", "http", rawCode, token.TokenHash))
	return rawCode, nil
}

func (s *service) Link(ctx context.Context, rawCode, provider, externalID string) (*domainidentity.ExternalIdentity, error) {
	rawCode = strings.TrimSpace(rawCode)
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	secrets := []string{rawCode}
	if rawCode != "" {
		hash := sha256.Sum256([]byte(rawCode))
		secrets = append(secrets, hex.EncodeToString(hash[:]))
	}
	if rawCode == "" {
		return s.linkFailure(ctx, provider, "", domainidentity.ErrInvalidLinkToken, secrets...)
	}
	if provider == "" {
		return s.linkFailure(ctx, provider, "", ErrInvalidIdentityProvider, secrets...)
	}
	if externalID == "" {
		return s.linkFailure(ctx, provider, "", ErrInvalidExternalIdentity, secrets...)
	}
	if s.repository == nil {
		return s.linkFailure(ctx, provider, "", errors.New("identity repository is not configured"), secrets...)
	}
	hash := sha256.Sum256([]byte(rawCode))
	tokenHash := hex.EncodeToString(hash[:])
	identity, err := s.repository.ConsumeAndLink(ctx, tokenHash, provider, externalID, s.now().UTC())
	if err != nil {
		return s.linkFailure(ctx, provider, "", err, secrets...)
	}
	if identity == nil || strings.TrimSpace(identity.ID) == "" || strings.TrimSpace(identity.UserID) == "" {
		return s.linkFailure(ctx, provider, "", domainidentity.ErrInvalidLinkToken, secrets...)
	}
	s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionCreate, "external_identity", identity.ID, identity.UserID, provider, domainaudit.StatusSuccess, "Linked external identity", "", "mcp", secrets...))
	return identity, nil
}

func (s *service) Revoke(ctx context.Context, userID, provider, externalID string) error {
	userID = strings.TrimSpace(userID)
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if userID == "" || provider == "" || externalID == "" {
		err := domainidentity.ErrIdentityNotFound
		s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionDelete, "external_identity", "", userID, provider, domainaudit.StatusFailed, "Failed to revoke external identity", FailureCategory(err), "http"))
		return err
	}
	if s.repository == nil {
		err := errors.New("identity repository is not configured")
		s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionDelete, "external_identity", "", userID, provider, domainaudit.StatusFailed, "Failed to revoke external identity", FailureCategory(err), "http"))
		return err
	}
	identity, err := s.repository.FindActive(ctx, provider, externalID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = domainidentity.ErrIdentityNotFound
	}
	if err != nil {
		s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionDelete, "external_identity", "", userID, provider, domainaudit.StatusFailed, "Failed to revoke external identity", FailureCategory(err), "http"))
		return err
	}
	if identity == nil || identity.Status != domainidentity.StatusActive || strings.TrimSpace(identity.UserID) != userID {
		err = domainidentity.ErrIdentityNotFound
		s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionDelete, "external_identity", "", userID, provider, domainaudit.StatusFailed, "Failed to revoke external identity", FailureCategory(err), "http"))
		return err
	}
	if err = s.repository.Revoke(ctx, userID, provider, externalID); err != nil {
		s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionDelete, "external_identity", identity.ID, userID, provider, domainaudit.StatusFailed, "Failed to revoke external identity", FailureCategory(err), "http"))
		return err
	}
	s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionDelete, "external_identity", identity.ID, userID, provider, domainaudit.StatusSuccess, "Revoked external identity", "", "http"))
	return nil
}

func newLinkCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *service) issueFailure(ctx context.Context, provider, userID string, err error, secrets ...string) (string, error) {
	s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionCreate, "identity_link_token", "", userID, provider, domainaudit.StatusFailed, "Failed to issue identity link code", FailureCategory(err), "http", secrets...))
	return "", err
}

func (s *service) linkFailure(ctx context.Context, provider, userID string, err error, secrets ...string) (*domainidentity.ExternalIdentity, error) {
	s.writeAudit(ctx, s.auditEvent(ctx, domainaudit.ActionCreate, "external_identity", "", userID, provider, domainaudit.StatusFailed, "Failed to link external identity", FailureCategory(err), "mcp", secrets...))
	return nil, err
}

func (s *service) auditEvent(ctx context.Context, action, resource, resourceID, subjectUserID, provider, status, message, failure, defaultSource string, secrets ...string) domainaudit.AuditEvent {
	scope := authscope.FromContext(ctx)
	actorUserID := scope.ActorUserID()
	if actorUserID == "" {
		actorUserID = strings.TrimSpace(subjectUserID)
	}
	provenance, _ := ctx.Value(auditProvenanceKey{}).(AuditProvenance)
	source := strings.TrimSpace(provenance.Source)
	if source == "" {
		source = defaultSource
	}
	metadata := utils.MergeMetadata(sanitizeAuditMetadata(provenance.Metadata, secrets...), map[string]any{"provider": provider})
	if scope.IsImpersonated && strings.TrimSpace(scope.UserID) != actorUserID && strings.TrimSpace(subjectUserID) != "" {
		metadata = utils.MergeMetadata(metadata, map[string]any{"subject_user_id": strings.TrimSpace(subjectUserID)})
	}
	return domainaudit.AuditEvent{
		Action:       action,
		Resource:     resource,
		ResourceID:   resourceID,
		ActorUserID:  actorUserID,
		ActorRole:    scope.ActorRole(),
		Source:       source,
		Status:       status,
		Message:      message,
		ErrorMessage: failure,
		RequestID:    provenance.RequestID,
		IPAddress:    provenance.IPAddress,
		UserAgent:    provenance.UserAgent,
		Metadata:     metadata,
	}
}

func sanitizeAuditMetadata(metadata map[string]any, secrets ...string) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	secretValues := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if strings.TrimSpace(secret) != "" {
			secretValues = append(secretValues, secret)
		}
	}
	cleaned, _ := sanitizeAuditValue(metadata, secretValues)
	result, _ := cleaned.(map[string]any)
	return result
}

func sanitizeAuditValue(value any, secrets []string) (any, bool) {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, nested := range current {
			if sensitiveAuditKey(key) {
				continue
			}
			if clean, ok := sanitizeAuditValue(nested, secrets); ok {
				out[key] = clean
			}
		}
		return out, true
	case []any:
		out := make([]any, 0, len(current))
		for _, nested := range current {
			if clean, ok := sanitizeAuditValue(nested, secrets); ok {
				out = append(out, clean)
			}
		}
		return out, true
	case string:
		clean := current
		for _, secret := range secrets {
			clean = strings.ReplaceAll(clean, secret, "[REDACTED]")
		}
		return clean, true
	default:
		return value, true
	}
}

func sensitiveAuditKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if utils.IsSensitiveKey(key) {
		return true
	}
	switch key {
	case "code", "link_code", "raw_code", "hash", "token_hash", "bearer", "bearer_key", "authorization", "body", "request_body", "payload", "raw_body":
		return true
	default:
		return false
	}
}

func FailureCategory(err error) string {
	switch {
	case errors.Is(err, ErrInvalidIdentityProvider), errors.Is(err, ErrInvalidExternalIdentity), errors.Is(err, domainidentity.ErrInvalidLinkToken):
		return "validation"
	case errors.Is(err, domainidentity.ErrIdentityConflict):
		return "conflict"
	case errors.Is(err, domainidentity.ErrIdentityNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "not_found"
	default:
		return "internal"
	}
}

func (s *service) writeAudit(ctx context.Context, event domainaudit.AuditEvent) {
	if s.audit != nil {
		_ = s.audit.Store(ctx, event)
	}
}

var _ LinkService = (*service)(nil)
