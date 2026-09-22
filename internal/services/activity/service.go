package serviceactivity

import (
	"context"
	"errors"
	"strings"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainaudit "family-assistant/internal/domain/audit"
	domainauthorization "family-assistant/internal/domain/authorization"
	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	interfaceauthorization "family-assistant/internal/interfaces/authorization"
	interfacespace "family-assistant/internal/interfaces/space"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
	"gorm.io/gorm"
)

const (
	createPermission = "activities:create"
	listPermission   = "activities:list"
	updatePermission = "activities:update"
	deletePermission = "activities:delete"
	requiredReason   = "is required"
	defaultLimit     = 50
	maxLimit         = 100
)

type auditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}

type service struct {
	activities interfaceactivity.RepoActivityInterface
	spaces     interfacespace.RepoSpaceInterface
	authorize  interfaceauthorization.Authorizer
	audit      auditStore
}

func NewService(activities interfaceactivity.RepoActivityInterface, spaces interfacespace.RepoSpaceInterface, authorize interfaceauthorization.Authorizer, audit auditStore) interfaceactivity.ServiceActivityInterface {
	return &service{activities: activities, spaces: spaces, authorize: authorize, audit: audit}
}

func (s *service) Create(ctx context.Context, actor domainidentity.ActorContext, input dto.ActivityCreateInput) (created *domainactivity.Activity, err error) {
	spaceID := strings.TrimSpace(input.Space)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	defer func() {
		if err != nil {
			event := activityAuditEvent(actor, domainaudit.ActionCreate, spaceID, "", domainaudit.StatusFailed)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()

	if strings.TrimSpace(input.Kind) == "" {
		return nil, &serviceauthorization.ValidationError{Field: "kind", Reason: requiredReason}
	}
	if strings.TrimSpace(input.Note) == "" {
		return nil, &serviceauthorization.ValidationError{Field: "note", Reason: requiredReason}
	}
	if input.OccurredAt.IsZero() {
		return nil, &serviceauthorization.ValidationError{Field: "occurred_at", Reason: requiredReason}
	}
	if s.authorize == nil || s.spaces == nil || s.activities == nil {
		return nil, errors.New("activity service is not configured")
	}
	if err := s.authorize.Authorize(ctx, actor, createPermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}
	if !s.hasMember(ctx, spaceID, actor.MemberID, actor.UserID) {
		return nil, serviceauthorization.ErrNotFound
	}

	now := time.Now().UTC()
	created = &domainactivity.Activity{
		ID:                utils.CreateUUID(),
		SpaceID:           spaceID,
		CreatedByMemberID: strings.TrimSpace(actor.MemberID),
		Kind:              strings.TrimSpace(input.Kind),
		Note:              strings.TrimSpace(input.Note),
		OccurredAt:        input.OccurredAt.UTC(),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.activities.Create(ctx, created); err != nil {
		return nil, err
	}
	s.writeAudit(ctx, activityAuditEvent(actor, domainaudit.ActionCreate, spaceID, created.ID, domainaudit.StatusSuccess))
	return created, nil
}

func (s *service) List(ctx context.Context, actor domainidentity.ActorContext, input dto.ActivityListInput) (activities []domainactivity.Activity, err error) {
	spaceID := strings.TrimSpace(input.Space)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	defer func() {
		if err != nil {
			event := activityAuditEvent(actor, "list", spaceID, "", domainaudit.StatusFailed)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()

	if s.authorize == nil || s.spaces == nil || s.activities == nil {
		return nil, errors.New("activity service is not configured")
	}
	if input.Limit < 0 {
		return nil, &serviceauthorization.ValidationError{Field: "limit", Reason: "must not be negative"}
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if err := s.authorize.Authorize(ctx, actor, listPermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}
	if !s.hasMember(ctx, spaceID, actor.MemberID, actor.UserID) {
		return nil, serviceauthorization.ErrNotFound
	}
	activities, err = s.activities.List(ctx, domainactivity.ListFilter{
		SpaceID: spaceID, Kind: strings.TrimSpace(input.Kind), From: input.From, To: input.To, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	s.writeAudit(ctx, activityAuditEvent(actor, "list", spaceID, "", domainaudit.StatusSuccess))
	return activities, nil
}

func (s *service) Update(ctx context.Context, actor domainidentity.ActorContext, spaceID, activityID string, input dto.ActivityUpdateInput) (result *domainactivity.Activity, err error) {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	activityID = strings.TrimSpace(activityID)
	defer func() {
		if err == nil {
			return
		}
		event := activityAuditEvent(actor, domainaudit.ActionUpdate, spaceID, activityID, domainaudit.StatusFailed)
		event.ErrorMessage = failureCategory(err)
		s.writeAudit(ctx, event)
	}()
	if s.authorize == nil || s.spaces == nil || s.activities == nil {
		return nil, errors.New("activity service is not configured")
	}
	if err := s.authorize.Authorize(ctx, actor, updatePermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	actorMember := activityMember(members, actor.MemberID, actor.UserID)
	if actorMember == nil {
		return nil, serviceauthorization.ErrNotFound
	}
	activity, err := s.activities.FindByIDInSpace(ctx, spaceID, activityID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, serviceauthorization.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if activity == nil {
		return nil, serviceauthorization.ErrNotFound
	}
	if !canMutateActivity(actorMember.RoleName, actorMember.ID, activity) {
		return nil, serviceauthorization.ErrForbidden
	}
	fields, err := activityUpdateFields(input)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := s.activities.Update(ctx, spaceID, activityID, fields, now); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, serviceauthorization.ErrNotFound
		}
		return nil, err
	}
	applyActivityUpdate(activity, fields, now)
	result = activity
	s.writeAudit(ctx, activityAuditEvent(actor, domainaudit.ActionUpdate, spaceID, activity.ID, domainaudit.StatusSuccess))
	return result, nil
}

func (s *service) Delete(ctx context.Context, actor domainidentity.ActorContext, spaceID, activityID string) (result *domainactivity.Activity, err error) {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	activityID = strings.TrimSpace(activityID)
	defer func() {
		if err == nil {
			return
		}
		event := activityAuditEvent(actor, domainaudit.ActionDelete, spaceID, activityID, domainaudit.StatusFailed)
		event.ErrorMessage = failureCategory(err)
		s.writeAudit(ctx, event)
	}()
	if s.authorize == nil || s.spaces == nil || s.activities == nil {
		return nil, errors.New("activity service is not configured")
	}
	if err := s.authorize.Authorize(ctx, actor, deletePermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	actorMember := activityMember(members, actor.MemberID, actor.UserID)
	if actorMember == nil {
		return nil, serviceauthorization.ErrNotFound
	}
	activity, err := s.activities.FindByIDInSpace(ctx, spaceID, activityID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, serviceauthorization.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if activity == nil {
		return nil, serviceauthorization.ErrNotFound
	}
	if !canMutateActivity(actorMember.RoleName, actorMember.ID, activity) {
		return nil, serviceauthorization.ErrForbidden
	}
	now := time.Now().UTC()
	if err := s.activities.SoftDelete(ctx, spaceID, activityID, now); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, serviceauthorization.ErrNotFound
		}
		return nil, err
	}
	activity.UpdatedAt = now
	activity.DeletedAt = gorm.DeletedAt{Time: now, Valid: true}
	result = activity
	s.writeAudit(ctx, activityAuditEvent(actor, domainaudit.ActionDelete, spaceID, activity.ID, domainaudit.StatusSuccess))
	return result, nil
}

func activityUpdateFields(input dto.ActivityUpdateInput) (domainactivity.UpdateFields, error) {
	fields := domainactivity.UpdateFields{}
	if input.Kind != nil {
		kind := strings.TrimSpace(*input.Kind)
		if kind == "" {
			return fields, &serviceauthorization.ValidationError{Field: "kind", Reason: requiredReason}
		}
		fields.Kind = &kind
	}
	if input.Note != nil {
		note := strings.TrimSpace(*input.Note)
		if note == "" {
			return fields, &serviceauthorization.ValidationError{Field: "note", Reason: requiredReason}
		}
		fields.Note = &note
	}
	if input.OccurredAt != nil {
		if input.OccurredAt.IsZero() {
			return fields, &serviceauthorization.ValidationError{Field: "occurred_at", Reason: requiredReason}
		}
		occurredAt := input.OccurredAt.UTC()
		fields.OccurredAt = &occurredAt
	}
	if fields.Kind == nil && fields.Note == nil && fields.OccurredAt == nil {
		return fields, &serviceauthorization.ValidationError{Field: "update", Reason: "at least one field is required"}
	}
	return fields, nil
}

func applyActivityUpdate(activity *domainactivity.Activity, fields domainactivity.UpdateFields, updatedAt time.Time) {
	if fields.Kind != nil {
		activity.Kind = *fields.Kind
	}
	if fields.Note != nil {
		activity.Note = *fields.Note
	}
	if fields.OccurredAt != nil {
		activity.OccurredAt = *fields.OccurredAt
	}
	activity.UpdatedAt = updatedAt
}

func activityMember(members []domainspace.ResolvedMembership, memberID, userID string) *domainspace.ResolvedMembership {
	memberID = strings.TrimSpace(memberID)
	userID = strings.TrimSpace(userID)
	for i := range members {
		member := &members[i]
		if strings.TrimSpace(member.Status) != domainspace.StatusActive {
			continue
		}
		if (memberID != "" && strings.TrimSpace(member.ID) == memberID) || (memberID == "" && strings.TrimSpace(member.UserID) == userID) {
			return member
		}
	}
	return nil
}

func canMutateActivity(role, memberID string, activity *domainactivity.Activity) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "space_owner", "space_admin":
		return true
	case "space_member":
		return strings.TrimSpace(activity.CreatedByMemberID) == strings.TrimSpace(memberID)
	default:
		return false
	}
}

func (s *service) hasMember(ctx context.Context, spaceID, memberID, userID string) bool {
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return false
	}
	memberID = strings.TrimSpace(memberID)
	userID = strings.TrimSpace(userID)
	for _, member := range members {
		if strings.TrimSpace(member.Status) != domainspace.StatusActive || strings.TrimSpace(member.SpaceID) != spaceID {
			continue
		}
		if (memberID != "" && strings.TrimSpace(member.ID) == memberID) || (memberID == "" && strings.TrimSpace(member.UserID) == userID) {
			return true
		}
	}
	return false
}

func activityAuditEvent(actor domainidentity.ActorContext, action, spaceID, resourceID, status string) domainaudit.AuditEvent {
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
		Action: action, Resource: "space_activity", ResourceID: strings.TrimSpace(resourceID), Status: status, Metadata: metadata,
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

var _ interfaceactivity.ServiceActivityInterface = (*service)(nil)
