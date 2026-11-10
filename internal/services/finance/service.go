package servicefinance

import (
	"context"
	"errors"
	"strings"
	"sync"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	interfaceauthorization "family-assistant/internal/interfaces/authorization"
	interfacefinance "family-assistant/internal/interfaces/finance"
	interfacespace "family-assistant/internal/interfaces/space"
	serviceauthorization "family-assistant/internal/services/authorization"
)

const (
	permList   = "finance:list"
	permManage = "finance:manage"

	requiredReason = "is required"
	defaultLimit   = 50
	maxLimit       = 100
	maxNameLen     = 128 // finance_accounts.name / finance_categories.name VARCHAR(128)

	resourceAccount  = "finance_account"
	resourceCategory = "finance_category"
	actionList       = "list"
)

type auditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}

type service struct {
	repo      interfacefinance.RepoFinanceInterface
	spaces    interfacespace.RepoSpaceInterface
	authorize interfaceauthorization.Authorizer
	audit     auditStore

	// seedLocks serializes default-category seeding per Space inside this
	// process; the schema has no unique (space, kind, name) index to rely on.
	seedLocks sync.Map
}

func NewService(repo interfacefinance.RepoFinanceInterface, spaces interfacespace.RepoSpaceInterface, authorize interfaceauthorization.Authorizer, audit auditStore) interfacefinance.ServiceFinanceInterface {
	return &service{repo: repo, spaces: spaces, authorize: authorize, audit: audit}
}

func (s *service) configured() error {
	if s.repo == nil || s.spaces == nil || s.authorize == nil {
		return errors.New("finance service is not configured")
	}
	return nil
}

// resolveSpace trims the requested Space and falls back to the actor's Space.
func resolveSpace(actor domainidentity.ActorContext, requested string) string {
	spaceID := strings.TrimSpace(requested)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	return spaceID
}

// clampLimit applies the list default and maximum. The repository does not.
func clampLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, &serviceauthorization.ValidationError{Field: "limit", Reason: "must not be negative"}
	}
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit > maxLimit {
		return maxLimit, nil
	}
	return limit, nil
}

func financeAuditEvent(actor domainidentity.ActorContext, action, resource, spaceID, resourceID, status string) domainaudit.AuditEvent {
	actorUserID := strings.TrimSpace(actor.UserID)
	actorRole := strings.TrimSpace(actor.RoleName)
	metadata := map[string]any{"space_id": strings.TrimSpace(spaceID)}
	if initiator := strings.TrimSpace(actor.InitiatorUserID); initiator != "" {
		actorUserID = initiator
		if actor.UserID != initiator {
			metadata["subject_user_id"] = strings.TrimSpace(actor.UserID)
		}
	}
	if role := strings.TrimSpace(actor.InitiatorRoleName); role != "" {
		actorRole = role
	}
	return domainaudit.AuditEvent{
		ActorUserID: actorUserID, ActorMemberID: strings.TrimSpace(actor.MemberID), ActorRole: actorRole,
		Source: auditSource(actor.Source), Channel: strings.TrimSpace(actor.Channel), AgentProfile: strings.TrimSpace(actor.ExternalID),
		Action: action, Resource: resource, ResourceID: strings.TrimSpace(resourceID), Status: status, Metadata: metadata,
	}
}

// auditResult writes the success or failed event for a finished operation.
func (s *service) auditResult(ctx context.Context, actor domainidentity.ActorContext, action, resource, spaceID, resourceID string, err error) {
	if err == nil {
		s.writeAudit(ctx, financeAuditEvent(actor, action, resource, spaceID, resourceID, domainaudit.StatusSuccess))
		return
	}
	event := financeAuditEvent(actor, action, resource, spaceID, resourceID, domainaudit.StatusFailed)
	event.ErrorMessage = failureCategory(err)
	s.writeAudit(ctx, event)
}

func (s *service) writeAudit(ctx context.Context, event domainaudit.AuditEvent) {
	if s.audit != nil {
		_ = s.audit.Store(ctx, event)
	}
}

func auditSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "mcp", "http":
		return strings.ToLower(strings.TrimSpace(source))
	default:
		return "unknown"
	}
}

func failureCategory(err error) string {
	var validationErr *serviceauthorization.ValidationError
	switch {
	case errors.As(err, &validationErr), errors.Is(err, serviceauthorization.ErrInvalidResource):
		return "validation"
	case errors.Is(err, serviceauthorization.ErrForbidden):
		return "forbidden"
	case errors.Is(err, serviceauthorization.ErrNotFound):
		return "not_found"
	default:
		return "internal"
	}
}

var _ interfacefinance.ServiceFinanceInterface = (*service)(nil)
