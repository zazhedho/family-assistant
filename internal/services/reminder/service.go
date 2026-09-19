package servicereminder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	domainfamilymember "github.com/zazhedho/family-assistant/internal/domain/familymember"
	domainreminder "github.com/zazhedho/family-assistant/internal/domain/reminder"
	"github.com/zazhedho/family-assistant/internal/services/authorization"
	identity "github.com/zazhedho/family-assistant/internal/services/identity"
	"github.com/zazhedho/family-assistant/pkg/logger"
	"gorm.io/gorm"
)

const (
	createPermission = "reminders:create"
	listPermission   = "reminders:list"
	updatePermission = "reminders:update"
)

var ErrConflict = errors.New("conflict")

type CreateInput struct {
	Title          string
	Description    string
	ScheduledAt    time.Time
	Scope          domainreminder.Scope
	TargetMemberID *string
}

type ListInput struct {
	Scope          *domainreminder.Scope
	Status         *domainreminder.Status
	From           *time.Time
	To             *time.Time
	TargetMemberID *string
}

type Service interface {
	Create(ctx context.Context, actor identity.ActorContext, input CreateInput) (*domainreminder.Reminder, error)
	List(ctx context.Context, actor identity.ActorContext, input ListInput) ([]domainreminder.Reminder, error)
	Complete(ctx context.Context, actor identity.ActorContext, reminderID string) (*domainreminder.Reminder, error)
}

type auditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}

type service struct {
	reminders domainreminder.Repository
	members   domainfamilymember.Repository
	authorize authorization.Authorizer
	audit     auditStore
}

func NewReminderService(
	reminders domainreminder.Repository,
	members domainfamilymember.Repository,
	authorize authorization.Authorizer,
	auditService auditStore,
) Service {
	return &service{
		reminders: reminders,
		members:   members,
		authorize: authorize,
		audit:     auditService,
	}
}

func NewService(
	reminders domainreminder.Repository,
	members domainfamilymember.Repository,
	authorize authorization.Authorizer,
	auditService auditStore,
) Service {
	return NewReminderService(reminders, members, authorize, auditService)
}

func (s *service) Create(ctx context.Context, actor identity.ActorContext, input CreateInput) (created *domainreminder.Reminder, err error) {
	ownerID := strings.TrimSpace(actor.MemberID)
	defer func() {
		if err == nil {
			return
		}
		resourceID := ""
		if created != nil {
			resourceID = auditResourceID(created.ID)
		}
		s.writeFailedAudit(ctx, actor, domainaudit.ActionCreate, resourceID, ownerID, err)
	}()

	if strings.TrimSpace(input.Title) == "" {
		return nil, &authorization.ValidationError{Field: "title", Reason: "is required"}
	}
	if input.ScheduledAt.IsZero() {
		return nil, &authorization.ValidationError{Field: "scheduled_at", Reason: "is required"}
	}

	scope := input.Scope
	if scope == "" {
		scope = domainreminder.ScopePersonal
	}
	if err := validateReminderScope(scope); err != nil {
		return nil, err
	}

	ownerRole := actor.RoleName
	if input.TargetMemberID != nil {
		target, err := s.targetMember(ctx, actor, input.TargetMemberID)
		if err != nil {
			return nil, err
		}
		ownerRole = target.RoleName
		if scope == domainreminder.ScopePersonal {
			ownerID = target.ID
		}
	}

	if err := s.authorize.Authorize(ctx, actor, createPermission, authorization.Resource{
		FamilyID:      actor.FamilyID,
		OwnerMemberID: ownerID,
		OwnerRoleName: ownerRole,
		Scope:         toAuthorizationScope(scope),
	}); err != nil {
		return nil, err
	}

	created = &domainreminder.Reminder{
		FamilyID:          actor.FamilyID,
		OwnerMemberID:     ownerID,
		CreatedByMemberID: actor.MemberID,
		Scope:             scope,
		Title:             input.Title,
		Description:       input.Description,
		ScheduledAt:       input.ScheduledAt,
		Status:            domainreminder.StatusPending,
	}
	if err := s.reminders.Create(ctx, created); err != nil {
		return nil, err
	}

	s.writeAudit(ctx, actor, domainaudit.AuditEvent{
		ActorUserID:           auditActorUserID(actor),
		ActorMemberID:         actor.MemberID,
		ResourceOwnerMemberID: created.OwnerMemberID,
		Source:                auditSource(actor),
		Channel:               auditChannel(actor),
		AgentProfile:          actor.HermesProfileID,
		ActorRole:             auditActorRole(actor),
		Action:                domainaudit.ActionCreate,
		Resource:              "reminder",
		ResourceID:            created.ID,
		Status:                domainaudit.StatusSuccess,
		Metadata:              reminderAuditMetadata(actor, created),
	})
	return created, nil
}

func (s *service) List(ctx context.Context, actor identity.ActorContext, input ListInput) ([]domainreminder.Reminder, error) {
	if err := validateListInput(input); err != nil {
		return nil, err
	}

	scope := domainreminder.ScopePersonal
	if input.Scope != nil {
		scope = *input.Scope
	}
	if err := validateReminderScope(scope); err != nil {
		return nil, err
	}

	ownerID := actor.MemberID
	ownerRole := actor.RoleName
	if input.TargetMemberID != nil {
		target, err := s.targetMember(ctx, actor, input.TargetMemberID)
		if err != nil {
			return nil, err
		}
		ownerID = target.ID
		ownerRole = target.RoleName
	}

	if err := s.authorize.Authorize(ctx, actor, listPermission, authorization.Resource{
		FamilyID:      actor.FamilyID,
		OwnerMemberID: ownerID,
		OwnerRoleName: ownerRole,
		Scope:         toAuthorizationScope(scope),
	}); err != nil {
		return nil, err
	}

	filter := domainreminder.ListFilter{
		FamilyID: actor.FamilyID,
		Scope:    scopePointer(scope),
		Status:   input.Status,
		From:     input.From,
		To:       input.To,
	}
	if scope == domainreminder.ScopePersonal {
		filter.OwnerMemberID = &ownerID
	}
	return s.reminders.List(ctx, filter)
}

func (s *service) Complete(ctx context.Context, actor identity.ActorContext, reminderID string) (result *domainreminder.Reminder, err error) {
	reminderID = strings.TrimSpace(reminderID)
	var reminder *domainreminder.Reminder
	defer func() {
		if err == nil {
			return
		}
		ownerID := ""
		if reminder != nil {
			ownerID = strings.TrimSpace(reminder.OwnerMemberID)
		}
		s.writeFailedAudit(ctx, actor, domainaudit.ActionUpdate, auditResourceID(reminderID), ownerID, err)
	}()

	if err := validateUUID(reminderID, "reminder_id"); err != nil {
		return nil, err
	}

	reminder, err = s.reminders.FindByIDInFamily(ctx, actor.FamilyID, reminderID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if reminder == nil {
		return nil, authorization.ErrNotFound
	}

	ownerRole := ""
	if reminder.Scope == domainreminder.ScopePersonal {
		if strings.TrimSpace(reminder.OwnerMemberID) == "" {
			return nil, authorization.ErrNotFound
		}
		owner, lookupErr := s.members.FindActiveByID(ctx, actor.FamilyID, reminder.OwnerMemberID)
		if lookupErr != nil {
			return nil, mapNotFound(lookupErr)
		}
		if owner == nil {
			return nil, authorization.ErrNotFound
		}
		ownerRole = owner.RoleName
	}
	if err := s.authorize.Authorize(ctx, actor, updatePermission, authorization.Resource{
		FamilyID:      actor.FamilyID,
		OwnerMemberID: reminder.OwnerMemberID,
		OwnerRoleName: ownerRole,
		Scope:         toAuthorizationScope(reminder.Scope),
	}); err != nil {
		return nil, err
	}
	if reminder.Status != domainreminder.StatusPending {
		return nil, fmt.Errorf("%w: reminder is already terminal", ErrConflict)
	}

	now := time.Now().UTC()
	reminder.Status = domainreminder.StatusCompleted
	reminder.CompletedAt = &now
	if err := s.reminders.Update(ctx, reminder); err != nil {
		return nil, mapUpdateError(err)
	}

	s.writeAudit(ctx, actor, domainaudit.AuditEvent{
		ActorUserID:           auditActorUserID(actor),
		ActorMemberID:         actor.MemberID,
		ResourceOwnerMemberID: reminder.OwnerMemberID,
		Source:                auditSource(actor),
		Channel:               auditChannel(actor),
		AgentProfile:          actor.HermesProfileID,
		ActorRole:             auditActorRole(actor),
		Action:                domainaudit.ActionUpdate,
		Resource:              "reminder",
		ResourceID:            reminder.ID,
		Status:                domainaudit.StatusSuccess,
		Metadata:              reminderAuditMetadata(actor, reminder),
	})
	return reminder, nil
}

func (s *service) targetMember(ctx context.Context, actor identity.ActorContext, targetID *string) (*domainfamilymember.FamilyMember, error) {
	id := strings.TrimSpace(*targetID)
	if err := validateUUID(id, "target_member_id"); err != nil {
		return nil, err
	}
	member, err := s.members.FindActiveByID(ctx, actor.FamilyID, id)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if member == nil {
		return nil, authorization.ErrNotFound
	}
	return member, nil
}

func validateReminderScope(scope domainreminder.Scope) error {
	switch scope {
	case domainreminder.ScopePersonal, domainreminder.ScopeFamily:
		return nil
	default:
		return &authorization.ValidationError{Field: "scope", Reason: "must be PERSONAL or FAMILY"}
	}
}

func validateReminderStatus(status domainreminder.Status) error {
	switch status {
	case domainreminder.StatusPending, domainreminder.StatusCompleted, domainreminder.StatusCancelled:
		return nil
	default:
		return &authorization.ValidationError{Field: "status", Reason: "must be PENDING, COMPLETED, or CANCELLED"} //nolint:misspell // persisted API enum; preserve spelling.
	}
}

func validateListInput(input ListInput) error {
	if input.Status != nil {
		if err := validateReminderStatus(*input.Status); err != nil {
			return err
		}
	}
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return &authorization.ValidationError{Field: "from", Reason: "must be before or equal to to"}
	}
	return nil
}

func validateUUID(value, field string) error {
	if value == "" {
		return &authorization.ValidationError{Field: field, Reason: "is required"}
	}
	if _, err := uuid.Parse(value); err != nil {
		return &authorization.ValidationError{Field: field, Reason: "must be a valid UUID"}
	}
	return nil
}

func toAuthorizationScope(scope domainreminder.Scope) authorization.Scope {
	return authorization.Scope(scope)
}

func scopePointer(scope domainreminder.Scope) *domainreminder.Scope {
	return &scope
}

func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authorization.ErrNotFound
	}
	return err
}

func mapUpdateError(err error) error {
	if errors.Is(err, domainreminder.ErrStatusConflict) {
		return fmt.Errorf("%w: reminder status changed", ErrConflict)
	}
	return mapNotFound(err)
}

func reminderAuditMetadata(actor identity.ActorContext, reminder *domainreminder.Reminder) map[string]any {
	metadata := map[string]any{
		"status": string(reminder.Status),
	}
	if subjectUserID := auditSubjectUserID(actor); subjectUserID != "" {
		metadata["subject_user_id"] = subjectUserID
	}
	return metadata
}

func auditActorUserID(actor identity.ActorContext) string {
	if strings.TrimSpace(actor.InitiatorUserID) != "" {
		return strings.TrimSpace(actor.InitiatorUserID)
	}
	return actor.UserID
}

func auditActorRole(actor identity.ActorContext) string {
	if strings.TrimSpace(actor.InitiatorRoleName) != "" {
		return strings.TrimSpace(actor.InitiatorRoleName)
	}
	return actor.RoleName
}

func auditSource(actor identity.ActorContext) string {
	source := strings.ToLower(strings.TrimSpace(actor.Source))
	if source == "mcp" || source == "http" {
		return source
	}
	return "unknown"
}

func auditChannel(actor identity.ActorContext) string {
	return strings.TrimSpace(actor.Channel)
}

func auditSubjectUserID(actor identity.ActorContext) string {
	initiatorUserID := strings.TrimSpace(actor.InitiatorUserID)
	subjectUserID := strings.TrimSpace(actor.UserID)
	if initiatorUserID != "" && initiatorUserID != subjectUserID {
		return subjectUserID
	}
	return ""
}

func auditResourceID(value string) string {
	value = strings.TrimSpace(value)
	if _, err := uuid.Parse(value); err != nil {
		return ""
	}
	return value
}

func auditFailureCategory(err error) string {
	var validationErr *authorization.ValidationError
	switch {
	case errors.As(err, &validationErr), errors.Is(err, authorization.ErrInvalidResource):
		return "validation"
	case errors.Is(err, authorization.ErrForbidden):
		return "forbidden"
	case errors.Is(err, authorization.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "not_found"
	case errors.Is(err, ErrConflict), errors.Is(err, domainreminder.ErrStatusConflict):
		return "conflict"
	default:
		return "internal"
	}
}

func (s *service) writeAudit(ctx context.Context, actor identity.ActorContext, event domainaudit.AuditEvent) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Store(ctx, event); err != nil {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("[Reminder][Audit]; failed to store audit trail: %v", err))
	}
}

func (s *service) writeFailedAudit(ctx context.Context, actor identity.ActorContext, action, resourceID, ownerID string, cause error) {
	category := auditFailureCategory(cause)
	s.writeAudit(ctx, actor, domainaudit.AuditEvent{
		ActorUserID:           auditActorUserID(actor),
		ActorMemberID:         strings.TrimSpace(actor.MemberID),
		ResourceOwnerMemberID: strings.TrimSpace(ownerID),
		Source:                auditSource(actor),
		Channel:               auditChannel(actor),
		AgentProfile:          strings.TrimSpace(actor.HermesProfileID),
		ActorRole:             auditActorRole(actor),
		Action:                action,
		Resource:              "reminder",
		ResourceID:            auditResourceID(resourceID),
		Status:                domainaudit.StatusFailed,
		ErrorMessage:          category,
		Metadata:              failureAuditMetadata(actor),
	})
}

func failureAuditMetadata(actor identity.ActorContext) map[string]any {
	if subjectUserID := auditSubjectUserID(actor); subjectUserID != "" {
		return map[string]any{"subject_user_id": subjectUserID}
	}
	return nil
}

var _ Service = (*service)(nil)
