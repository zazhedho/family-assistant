package servicereminder

import (
	"context"
	"errors"
	"strings"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainauthorization "family-assistant/internal/domain/authorization"
	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceauthorization "family-assistant/internal/interfaces/authorization"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	interfacespace "family-assistant/internal/interfaces/space"
	"family-assistant/internal/services/authorization"
	"gorm.io/gorm"
)

const (
	createPermission = "reminders:create"
	listPermission   = "reminders:list"
	updatePermission = "reminders:update"
)

var ErrConflict = errors.New("conflict")

type auditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}

type service struct {
	reminders interfacereminder.RepoReminderInterface
	spaces    interfacespace.RepoSpaceInterface
	authorize interfaceauthorization.Authorizer
	audit     auditStore
}

func NewReminderService(reminders interfacereminder.RepoReminderInterface, spaces interfacespace.RepoSpaceInterface, authorize interfaceauthorization.Authorizer, audit auditStore) interfacereminder.ServiceReminderInterface {
	return &service{reminders: reminders, spaces: spaces, authorize: authorize, audit: audit}
}

func (s *service) Create(ctx context.Context, actor domainidentity.ActorContext, input dto.ReminderCreateInput) (created *domainreminder.Reminder, err error) {
	spaceID := strings.TrimSpace(input.Space)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	defer func() {
		if err == nil {
			return
		}
		ownerID := strings.TrimSpace(actor.MemberID)
		resourceID := ""
		if created != nil {
			ownerID = strings.TrimSpace(created.CreatedByMemberID)
			resourceID = strings.TrimSpace(created.ID)
		}
		event := auditEvent(actor, domainaudit.ActionCreate, spaceID, resourceID, ownerID, domainaudit.StatusFailed, nil)
		event.ErrorMessage = failureCategory(err)
		s.writeAudit(ctx, event)
	}()

	if strings.TrimSpace(input.Title) == "" {
		return nil, &authorization.ValidationError{Field: "title", Reason: "is required"}
	}
	if input.ScheduledAt.IsZero() {
		return nil, &authorization.ValidationError{Field: "scheduled_at", Reason: "is required"}
	}

	if err := s.authorize.Authorize(ctx, actor, createPermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}

	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if !hasMember(members, actor.MemberID, actor.UserID) {
		return nil, authorization.ErrNotFound
	}
	if input.AssigneeMemberID != nil {
		assigneeID := strings.TrimSpace(*input.AssigneeMemberID)
		if !hasMemberID(members, assigneeID) {
			return nil, authorization.ErrNotFound
		}
		input.AssigneeMemberID = &assigneeID
	}

	created = &domainreminder.Reminder{
		SpaceID:           spaceID,
		CreatedByMemberID: strings.TrimSpace(actor.MemberID),
		AssigneeMemberID:  input.AssigneeMemberID,
		Title:             strings.TrimSpace(input.Title),
		Description:       input.Description,
		ScheduledAt:       input.ScheduledAt,
		Status:            domainreminder.StatusPending,
	}
	if err := s.reminders.Create(ctx, created); err != nil {
		return nil, err
	}
	s.writeAudit(ctx, auditEvent(actor, domainaudit.ActionCreate, created.SpaceID, created.ID, created.CreatedByMemberID, domainaudit.StatusSuccess, nil))
	return created, nil
}

func (s *service) List(ctx context.Context, actor domainidentity.ActorContext, input dto.ReminderListInput) (result []domainreminder.Reminder, err error) {
	spaceID := strings.TrimSpace(input.Space)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	defer func() {
		if err == nil {
			return
		}
		event := auditEvent(actor, "list", spaceID, "", actor.MemberID, domainaudit.StatusFailed, nil)
		event.ErrorMessage = failureCategory(err)
		s.writeAudit(ctx, event)
	}()
	if err := s.authorize.Authorize(ctx, actor, listPermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if !hasMember(members, actor.MemberID, actor.UserID) {
		return nil, authorization.ErrNotFound
	}
	result, err = s.reminders.List(ctx, domainreminder.ListFilter{SpaceID: spaceID, Status: input.Status, From: input.From, To: input.To})
	if err != nil {
		return nil, err
	}
	s.writeAudit(ctx, auditEvent(actor, "list", spaceID, "", actor.MemberID, domainaudit.StatusSuccess, nil))
	return result, nil
}

func (s *service) Complete(ctx context.Context, actor domainidentity.ActorContext, spaceID, reminderID string) (result *domainreminder.Reminder, err error) {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	reminderID = strings.TrimSpace(reminderID)
	defer func() {
		if err == nil {
			return
		}
		ownerID := strings.TrimSpace(actor.MemberID)
		if result != nil {
			ownerID = strings.TrimSpace(result.CreatedByMemberID)
		}
		event := auditEvent(actor, domainaudit.ActionUpdate, spaceID, reminderID, ownerID, domainaudit.StatusFailed, nil)
		event.ErrorMessage = failureCategory(err)
		s.writeAudit(ctx, event)
	}()
	if err := s.authorize.Authorize(ctx, actor, updatePermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	actorMember := activeMember(members, actor.MemberID, actor.UserID)
	if actorMember == nil {
		return nil, authorization.ErrNotFound
	}
	reminder, err := s.reminders.FindByIDInSpace(ctx, spaceID, reminderID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, authorization.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if reminder == nil {
		return nil, authorization.ErrNotFound
	}
	if !canComplete(actorMember.RoleName, actorMember.ID, reminder) {
		return nil, authorization.ErrForbidden
	}
	if reminder.Status != domainreminder.StatusPending {
		return nil, ErrConflict
	}
	now := time.Now().UTC()
	if err := s.reminders.CompletePending(ctx, spaceID, reminderID, now); err != nil {
		if errors.Is(err, domainreminder.ErrStatusConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	reminder.Status = domainreminder.StatusCompleted
	reminder.CompletedAt = &now
	result = reminder
	s.writeAudit(ctx, auditEvent(actor, domainaudit.ActionUpdate, spaceID, reminder.ID, reminder.CreatedByMemberID, domainaudit.StatusSuccess, nil))
	return result, nil
}

func hasMember(members []domainspace.ResolvedMembership, memberID, userID string) bool {
	memberID = strings.TrimSpace(memberID)
	if memberID != "" {
		return hasMemberID(members, memberID)
	}
	for _, member := range members {
		if strings.TrimSpace(member.Status) == domainspace.StatusActive && strings.TrimSpace(member.UserID) == strings.TrimSpace(userID) {
			return true
		}
	}
	return false
}

func hasMemberID(members []domainspace.ResolvedMembership, memberID string) bool {
	for _, member := range members {
		if strings.TrimSpace(member.Status) == domainspace.StatusActive && strings.TrimSpace(member.ID) == memberID {
			return true
		}
	}
	return false
}

func activeMember(members []domainspace.ResolvedMembership, memberID, userID string) *domainspace.ResolvedMembership {
	memberID = strings.TrimSpace(memberID)
	for i := range members {
		member := &members[i]
		if strings.TrimSpace(member.Status) != domainspace.StatusActive {
			continue
		}
		if (memberID != "" && strings.TrimSpace(member.ID) == memberID) || (memberID == "" && strings.TrimSpace(member.UserID) == strings.TrimSpace(userID)) {
			return member
		}
	}
	return nil
}

func canComplete(role, memberID string, reminder *domainreminder.Reminder) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "space_owner", "space_admin":
		return true
	case "space_member":
		if strings.TrimSpace(reminder.CreatedByMemberID) == strings.TrimSpace(memberID) {
			return true
		}
		return reminder.AssigneeMemberID != nil && strings.TrimSpace(*reminder.AssigneeMemberID) == strings.TrimSpace(memberID)
	default:
		return false
	}
}

func auditEvent(actor domainidentity.ActorContext, action, spaceID, resourceID, ownerID, status string, metadata map[string]any) domainaudit.AuditEvent {
	if metadata == nil {
		metadata = make(map[string]any, 2)
	}
	metadata["space_id"] = strings.TrimSpace(spaceID)
	actorUserID := strings.TrimSpace(actor.UserID)
	actorRole := strings.TrimSpace(actor.RoleName)
	initiatorUserID := strings.TrimSpace(actor.InitiatorUserID)
	if initiatorUserID != "" {
		actorUserID = initiatorUserID
		if subjectUserID := strings.TrimSpace(actor.UserID); subjectUserID != "" && subjectUserID != initiatorUserID {
			metadata["subject_user_id"] = subjectUserID
		}
	}
	if initiatorRole := strings.TrimSpace(actor.InitiatorRoleName); initiatorRole != "" {
		actorRole = initiatorRole
	}
	profile := strings.TrimSpace(actor.ExternalID)
	return domainaudit.AuditEvent{
		ActorUserID:           actorUserID,
		ActorMemberID:         strings.TrimSpace(actor.MemberID),
		ResourceOwnerMemberID: strings.TrimSpace(ownerID),
		Source:                auditSource(actor.Source),
		Channel:               strings.TrimSpace(actor.Channel),
		AgentProfile:          profile,
		ActorRole:             actorRole,
		Action:                action,
		Resource:              "reminder",
		ResourceID:            strings.TrimSpace(resourceID),
		Status:                status,
		Metadata:              metadata,
	}
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
	var validationErr *authorization.ValidationError
	switch {
	case errors.As(err, &validationErr), errors.Is(err, authorization.ErrInvalidResource):
		return "validation"
	case errors.Is(err, authorization.ErrForbidden):
		return "forbidden"
	case errors.Is(err, authorization.ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrConflict), errors.Is(err, domainreminder.ErrStatusConflict):
		return "conflict"
	default:
		return "internal"
	}
}
