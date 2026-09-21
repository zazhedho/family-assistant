package serviceonboarding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"
	domainrole "family-assistant/internal/domain/role"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/config"
	"family-assistant/utils"
)

const (
	StatusCreated  = "created"
	StatusExisting = "existing"
)

type Input struct {
	Name       string
	BirthDate  string
	Consent    bool
	Provider   string
	ExternalID string
	Channel    string
}

type Result struct {
	Status  string
	UserID  string
	SpaceID string
}

type Registrar interface {
	Register(context.Context, Input) (Result, error)
}

type RoleFinder interface {
	GetByName(context.Context, string) (domainrole.Role, error)
}

type AuditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}

type Service struct {
	repository domainonboarding.Repository
	roles      RoleFinder
	audit      AuditStore
	now        func() time.Time
}

func NewService(repository domainonboarding.Repository, roles RoleFinder, audit AuditStore) *Service {
	return &Service{
		repository: repository,
		roles:      roles,
		audit:      audit,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Register(ctx context.Context, input Input) (Result, error) {
	provider := domainidentity.NormalizeProvider(input.Provider)
	externalID := strings.TrimSpace(input.ExternalID)
	channel := strings.TrimSpace(input.Channel)
	if provider == "" || externalID == "" {
		return Result{}, s.fail(ctx, channel, provider, serviceidentity.ErrUnauthenticated)
	}
	if !input.Consent {
		return Result{}, s.fail(ctx, channel, provider, &serviceidentity.ValidationError{Field: "consent", Reason: "is required"})
	}

	name := utils.TitleCase(utils.StripHTML(input.Name))
	if count := utf8.RuneCountInString(name); count < 3 || count > 100 {
		return Result{}, s.fail(ctx, channel, provider, &serviceidentity.ValidationError{Field: "name", Reason: "must contain 3 to 100 characters"})
	}
	birthDate, err := parseBirthDate(input.BirthDate, s.now().UTC())
	if err != nil {
		return Result{}, s.fail(ctx, channel, provider, err)
	}
	if s.repository == nil {
		return Result{}, s.fail(ctx, channel, provider, errors.New("onboarding repository is not configured"))
	}

	existing, err := s.repository.FindByExternalIdentity(ctx, provider, externalID)
	if err == nil {
		if strings.TrimSpace(existing.UserID) == "" || strings.TrimSpace(existing.SpaceID) == "" {
			return Result{}, s.fail(ctx, channel, provider, errors.New("existing account is not configured"))
		}
		return Result{Status: StatusExisting, UserID: existing.UserID, SpaceID: existing.SpaceID}, nil
	}
	if !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		return Result{}, s.fail(ctx, channel, provider, err)
	}
	if s.roles == nil {
		return Result{}, s.fail(ctx, channel, provider, errors.New("onboarding roles are not configured"))
	}

	viewer, err := s.roles.GetByName(ctx, utils.RoleViewer)
	if err != nil || strings.TrimSpace(viewer.Id) == "" {
		return Result{}, s.fail(ctx, channel, provider, errors.New("role viewer is not configured"))
	}
	owner, err := s.roles.GetByName(ctx, "space_owner")
	if err != nil || strings.TrimSpace(owner.Id) == "" {
		return Result{}, s.fail(ctx, channel, provider, errors.New("role space_owner is not configured"))
	}

	registration := s.newRegistration(name, birthDate, provider, externalID, viewer.Id, owner.Id)
	if err := s.repository.Create(ctx, registration); err != nil {
		if errors.Is(err, domainidentity.ErrIdentityConflict) {
			winner, findErr := s.repository.FindByExternalIdentity(ctx, provider, externalID)
			if findErr == nil && strings.TrimSpace(winner.UserID) != "" && strings.TrimSpace(winner.SpaceID) != "" {
				return Result{Status: StatusExisting, UserID: winner.UserID, SpaceID: winner.SpaceID}, nil
			}
		}
		return Result{}, s.fail(ctx, channel, provider, err)
	}
	s.success(ctx, channel, provider, registration.User.Id)
	return Result{Status: StatusCreated, UserID: registration.User.Id, SpaceID: registration.Space.ID}, nil
}

func parseBirthDate(raw string, now time.Time) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, &serviceidentity.ValidationError{Field: "birth_date", Reason: "is required"}
	}
	birthDate, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, &serviceidentity.ValidationError{Field: "birth_date", Reason: "must use YYYY-MM-DD"}
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if birthDate.After(today) {
		return nil, &serviceidentity.ValidationError{Field: "birth_date", Reason: "cannot be in the future"}
	}
	if config.AgeOn(birthDate, today) < config.LoadMinimumIndependentAccountAge() {
		return nil, &serviceidentity.ValidationError{
			Field:  "birth_date",
			Reason: fmt.Sprintf("must be at least %d years old", config.LoadMinimumIndependentAccountAge()),
		}
	}
	return &birthDate, nil
}

func (s *Service) newRegistration(name string, birthDate *time.Time, provider, externalID, viewerRoleID, ownerRoleID string) domainonboarding.Registration {
	now := s.now().UTC()
	userID := utils.CreateUUID()
	spaceID := utils.CreateUUID()
	viewerRoleID = strings.TrimSpace(viewerRoleID)
	registration := domainonboarding.Registration{
		User: domainuser.Users{
			Id:                    userID,
			Name:                  name,
			Role:                  utils.RoleViewer,
			RoleId:                &viewerRoleID,
			LoginProvider:         provider,
			Metadata:              map[string]any{},
			BirthDate:             birthDate,
			AgeVerificationMethod: "self_declared",
			AgeVerifiedAt:         &now,
			PersonalSpaceID:       spaceID,
			CreatedAt:             now,
		},
		Space: domainspace.Space{
			ID:              spaceID,
			Name:            name + "'s Space",
			Type:            domainspace.TypePersonal,
			Category:        domainspace.CategoryPersonal,
			Status:          domainspace.StatusActive,
			CreatedByUserID: userID,
			CreatedAt:       now,
		},
		Member: domainspace.Member{
			ID:        utils.CreateUUID(),
			SpaceID:   spaceID,
			UserID:    userID,
			RoleID:    strings.TrimSpace(ownerRoleID),
			Status:    domainspace.StatusActive,
			CreatedAt: now,
		},
		Identity: domainidentity.ExternalIdentity{
			ID:         utils.CreateUUID(),
			UserID:     userID,
			Provider:   provider,
			ExternalID: externalID,
			Status:     domainidentity.StatusActive,
			VerifiedAt: now,
			Metadata:   map[string]any{},
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
	return registration
}

func (s *Service) success(ctx context.Context, channel, provider, userID string) {
	event := s.auditEvent(channel, provider, domainaudit.StatusSuccess, userID)
	if s.audit != nil {
		_ = s.audit.Store(ctx, event)
	}
}

func (s *Service) fail(ctx context.Context, channel, provider string, err error) error {
	event := s.auditEvent(channel, provider, domainaudit.StatusFailed, "")
	event.ErrorMessage = failureCategory(err)
	if s.audit != nil {
		_ = s.audit.Store(ctx, event)
	}
	return err
}

func (s *Service) auditEvent(channel, provider, status, actorUserID string) domainaudit.AuditEvent {
	channel = strings.TrimSpace(channel)
	return domainaudit.AuditEvent{
		OccurredAt:  s.now().UTC(),
		ActorUserID: actorUserID,
		Channel:     channel,
		Action:      domainaudit.ActionCreate,
		Resource:    "external_account",
		ResourceID:  actorUserID,
		Status:      status,
		Metadata: map[string]any{
			"provider": provider,
			"channel":  channel,
		},
	}
}

func failureCategory(err error) string {
	var validationErr *serviceidentity.ValidationError
	switch {
	case errors.As(err, &validationErr):
		return "validation"
	case errors.Is(err, serviceidentity.ErrUnauthenticated):
		return "unauthenticated"
	case errors.Is(err, domainidentity.ErrIdentityConflict):
		return "conflict"
	default:
		return "internal"
	}
}

var _ Registrar = (*Service)(nil)
