package servicescheduledmessage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainauthorization "family-assistant/internal/domain/authorization"
	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceaudit "family-assistant/internal/interfaces/audit"
	interfaceauthorization "family-assistant/internal/interfaces/authorization"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	interfacespace "family-assistant/internal/interfaces/space"
	"family-assistant/internal/services/authorization"
	"family-assistant/pkg/logger"
	"family-assistant/utils"
	"gorm.io/gorm"
)

const (
	createPermission        = "scheduled_messages:create"
	listPermission          = "scheduled_messages:list"
	viewPermission          = "scheduled_messages:view"
	updatePermission        = "scheduled_messages:update"
	cancelPermission        = "scheduled_messages:delete"
	unsubscribePermission   = "scheduled_messages:unsubscribe"
	bindPermission          = "space_channels:create"
	maxContentBytes         = 2000
	maxListRows             = 100
	invalidValueReason      = "is invalid"
	validIANATimezoneReason = "must be a valid IANA timezone"
)

type service struct {
	repository   interfacescheduledmessage.Repository
	spaces       interfacespace.RepoSpaceInterface
	authorize    interfaceauthorization.Authorizer
	audit        interfaceaudit.ServiceAuditInterface
	index        interfacescheduledmessage.DueIndexWriter
	generator    interfacescheduledmessage.Generator
	maxTextBytes int
	maxListRows  int
}

type scheduledMessageAuditScope struct {
	spaceID    string
	resourceID string
	ownerID    string
}

type ScheduledMessageServiceOptions struct {
	MaxTextBytes int
	MaxListRows  int
}

func NewScheduledMessageService(
	repository interfacescheduledmessage.Repository,
	spaces interfacespace.RepoSpaceInterface,
	authorize interfaceauthorization.Authorizer,
	audit interfaceaudit.ServiceAuditInterface,
	index interfacescheduledmessage.DueIndexWriter,
	generator interfacescheduledmessage.Generator,
	options ScheduledMessageServiceOptions,
) interfacescheduledmessage.Service {
	if options.MaxTextBytes <= 0 {
		options.MaxTextBytes = maxContentBytes
	}
	if options.MaxListRows <= 0 || options.MaxListRows > maxListRows {
		options.MaxListRows = maxListRows
	}
	return &service{
		repository: repository, spaces: spaces, authorize: authorize, audit: audit,
		index: index, generator: generator, maxTextBytes: options.MaxTextBytes, maxListRows: options.MaxListRows,
	}
}

func (s *service) Create(ctx context.Context, actor domainidentity.ActorContext, input dto.ScheduledMessageCreateInput) (created *domainscheduledmessage.Schedule, err error) {
	spaceID := strings.TrimSpace(actor.SpaceID)
	defer func() {
		if err != nil {
			event := s.auditEvent(ctx, actor, domainaudit.ActionCreate, scheduledMessageAuditScope{spaceID: spaceID, resourceID: scheduleID(created), ownerID: actor.MemberID}, domainaudit.StatusFailed, nil)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()

	requestedSpace, resolveErr := resolveSpaceID(input.Space, actor.SpaceID)
	if resolveErr != nil {
		return nil, resolveErr
	}
	spaceID = requestedSpace
	actorMember, members, err := s.authorizedMember(ctx, actor, spaceID, createPermission)
	if err != nil {
		return nil, err
	}
	if input.ContentMode != domainscheduledmessage.ContentModeFixed && input.ContentMode != domainscheduledmessage.ContentModeAI {
		return nil, invalid("content_mode", "must be FIXED or AI")
	}
	content, err := validateContent(input.Content, s.maxTextBytes)
	if err != nil {
		return nil, err
	}
	if input.ContentMode == domainscheduledmessage.ContentModeAI && s.generator == nil {
		return nil, invalid("content_mode", "AI generation is unavailable")
	}
	startAt, err := validateFutureTime(input.StartAt)
	if err != nil {
		return nil, err
	}
	timezone, err := validateTimezone(input.Timezone)
	if err != nil {
		return nil, err
	}
	if err := validateRecurrence(input.Recurrence, startAt, time.Now().UTC(), timezone); err != nil {
		return nil, err
	}
	recipientID, channelID, err := s.resolveCreateTarget(ctx, spaceID, input, members)
	if err != nil {
		return nil, err
	}
	created = &domainscheduledmessage.Schedule{
		ID: utils.CreateUUID(), SpaceID: spaceID, CreatedByMemberID: actorMember.ID,
		Target: input.Target, RecipientMemberID: recipientID, SpaceChannelID: channelID,
		ContentMode: input.ContentMode, Content: content, Recurrence: input.Recurrence,
		Timezone: timezone, StartAt: startAt, NextScheduledAt: &startAt,
		Status: domainscheduledmessage.ScheduleStatusActive,
	}
	delivery := &domainscheduledmessage.Delivery{
		ID: utils.CreateUUID(), ScheduledMessageID: created.ID, ScheduledAt: startAt,
		NextAttemptAt: &startAt, Status: domainscheduledmessage.DeliveryStatusPending,
	}
	if created.ContentMode == domainscheduledmessage.ContentModeFixed {
		delivery.ContentSnapshot = &content
	}
	finishIndexMutation := s.beginIndexMutation()
	if err := s.repository.Create(ctx, created, delivery); err != nil {
		finishIndexMutation(err)
		return nil, err
	}
	indexErr := s.upsertIndex(ctx, delivery.ID, startAt)
	finishIndexMutation(indexErr)
	s.writeAudit(ctx, s.auditEvent(ctx, actor, domainaudit.ActionCreate, scheduledMessageAuditScope{spaceID: spaceID, resourceID: created.ID, ownerID: created.CreatedByMemberID}, domainaudit.StatusSuccess, map[string]any{"target": created.Target, "status": created.Status}))
	return created, nil
}

func (s *service) List(ctx context.Context, actor domainidentity.ActorContext, input dto.ScheduledMessageListInput) (result []dto.ScheduledMessageOutput, err error) {
	spaceID := strings.TrimSpace(actor.SpaceID)
	resourceID := ""
	defer func() {
		if err != nil {
			event := s.auditEvent(ctx, actor, "list", scheduledMessageAuditScope{spaceID: spaceID, resourceID: resourceID, ownerID: actor.MemberID}, domainaudit.StatusFailed, nil)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()
	requestedSpace, resolveErr := resolveSpaceID(input.Space, actor.SpaceID)
	if resolveErr != nil {
		return nil, resolveErr
	}
	spaceID = requestedSpace
	permission, action := listPermission, "list"
	var scheduleID *string
	if input.ScheduleID != nil {
		id, parseErr := parseUUID(*input.ScheduleID, "schedule_id")
		if parseErr != nil {
			return nil, parseErr
		}
		scheduleID = &id
		resourceID = id
		permission, action = viewPermission, "view"
	}
	actorMember, _, err := s.authorizedMember(ctx, actor, spaceID, permission)
	if err != nil {
		return nil, err
	}
	if input.Status != nil && !validScheduleStatus(*input.Status) {
		return nil, invalid("status", invalidValueReason)
	}
	query := dto.ScheduledMessageListInput{
		Space: spaceID, ScheduleID: scheduleID, Status: input.Status,
		Limit: s.maxListRows, ViewerMemberID: actorMember.ID, CanViewAllDMs: isOwnerOrAdmin(actorMember.RoleName),
	}
	schedules, err := s.repository.List(ctx, query)
	if err != nil {
		return nil, err
	}
	result = make([]dto.ScheduledMessageOutput, 0, len(schedules))
	for i := range schedules {
		schedule := &schedules[i]
		if schedule.SpaceID != spaceID || !domainscheduledmessage.CanRead(schedule.Target, actorMember.RoleName, actorMember.ID, schedule.CreatedByMemberID, memberID(schedule.RecipientMemberID)) {
			continue
		}
		result = append(result, scheduledMessageOutput(schedule))
		if len(result) == s.maxListRows {
			break
		}
	}
	if err := s.loadScheduleHistory(ctx, spaceID, scheduleID, result); err != nil {
		return nil, err
	}
	s.writeAudit(ctx, s.auditEvent(ctx, actor, action, scheduledMessageAuditScope{spaceID: spaceID, resourceID: resourceID, ownerID: actorMember.ID}, domainaudit.StatusSuccess, nil))
	return result, nil
}

func (s *service) loadScheduleHistory(ctx context.Context, spaceID string, scheduleID *string, result []dto.ScheduledMessageOutput) error {
	if scheduleID == nil || len(result) != 1 {
		return nil
	}
	history, err := s.repository.ListHistory(ctx, spaceID, *scheduleID, s.maxListRows)
	if err != nil {
		return err
	}
	result[0].History = make([]dto.ScheduledMessageDeliveryOutput, 0, min(len(history), s.maxListRows))
	for i := 0; i < len(history) && i < s.maxListRows; i++ {
		result[0].History = append(result[0].History, deliveryOutput(&history[i]))
	}
	return nil
}

func (s *service) Update(ctx context.Context, actor domainidentity.ActorContext, rawSpaceID, rawScheduleID string, input dto.ScheduledMessageUpdateInput) (result *domainscheduledmessage.Schedule, err error) {
	spaceID := strings.TrimSpace(actor.SpaceID)
	scheduleID := ""
	defer func() {
		if err != nil {
			event := s.auditEvent(ctx, actor, domainaudit.ActionUpdate, scheduledMessageAuditScope{spaceID: spaceID, resourceID: scheduleID, ownerID: actor.MemberID}, domainaudit.StatusFailed, nil)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()
	var resolveErr error
	spaceID, resolveErr = resolveSpaceID(rawSpaceID, actor.SpaceID)
	if resolveErr != nil {
		return nil, resolveErr
	}
	scheduleID, err = parseUUID(rawScheduleID, "schedule_id")
	if err != nil {
		return nil, err
	}
	actorMember, _, err := s.authorizedMember(ctx, actor, spaceID, updatePermission)
	if err != nil {
		return nil, err
	}
	schedule, err := s.findSchedule(ctx, spaceID, scheduleID)
	if err != nil {
		return nil, err
	}
	if schedule.Status != domainscheduledmessage.ScheduleStatusActive {
		return nil, interfacescheduledmessage.ErrConflict
	}
	if !domainscheduledmessage.CanUpdate(schedule.Target, actorMember.RoleName, actorMember.ID, schedule.CreatedByMemberID) {
		return nil, authorization.ErrForbidden
	}
	fields, nextAt, contentMode, content, recurrence, timezone, startAt, err := s.updateFields(schedule, input, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	next := &domainscheduledmessage.Delivery{
		ID: utils.CreateUUID(), ScheduledMessageID: schedule.ID, ScheduledAt: nextAt,
		NextAttemptAt: &nextAt, Status: domainscheduledmessage.DeliveryStatusPending,
	}
	if contentMode == domainscheduledmessage.ContentModeFixed {
		next.ContentSnapshot = &content
	}
	now := time.Now().UTC()
	finishIndexMutation := s.beginIndexMutation()
	cancelledDeliveryIDs, err := s.repository.UpdateActive(ctx, spaceID, scheduleID, fields, next, now)
	if err != nil {
		finishIndexMutation(err)
		return nil, err
	}
	schedule.ContentMode, schedule.Content = contentMode, content
	schedule.Recurrence, schedule.Timezone, schedule.StartAt = recurrence, timezone, startAt
	schedule.NextScheduledAt, schedule.UpdatedAt = &nextAt, now
	var indexErr error
	for _, deliveryID := range cancelledDeliveryIDs {
		indexErr = errors.Join(indexErr, s.removeFromIndex(ctx, deliveryID))
	}
	indexErr = errors.Join(indexErr, s.upsertIndex(ctx, next.ID, nextAt))
	finishIndexMutation(indexErr)
	s.writeAudit(ctx, s.auditEvent(ctx, actor, domainaudit.ActionUpdate, scheduledMessageAuditScope{spaceID: spaceID, resourceID: schedule.ID, ownerID: schedule.CreatedByMemberID}, domainaudit.StatusSuccess, map[string]any{"target": schedule.Target, "status": schedule.Status}))
	return schedule, nil
}

func (s *service) Cancel(ctx context.Context, actor domainidentity.ActorContext, rawSpaceID, rawScheduleID string) (result *domainscheduledmessage.Schedule, err error) {
	spaceID := strings.TrimSpace(actor.SpaceID)
	scheduleID := ""
	defer func() {
		if err != nil {
			event := s.auditEvent(ctx, actor, domainaudit.ActionDelete, scheduledMessageAuditScope{spaceID: spaceID, resourceID: scheduleID, ownerID: actor.MemberID}, domainaudit.StatusFailed, nil)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()
	var resolveErr error
	spaceID, resolveErr = resolveSpaceID(rawSpaceID, actor.SpaceID)
	if resolveErr != nil {
		return nil, resolveErr
	}
	scheduleID, err = parseUUID(rawScheduleID, "schedule_id")
	if err != nil {
		return nil, err
	}
	actorMember, _, err := s.authorizedMember(ctx, actor, spaceID, cancelPermission)
	if err != nil {
		return nil, err
	}
	schedule, err := s.findSchedule(ctx, spaceID, scheduleID)
	if err != nil {
		return nil, err
	}
	if schedule.Status != domainscheduledmessage.ScheduleStatusActive {
		return nil, interfacescheduledmessage.ErrConflict
	}
	if !domainscheduledmessage.CanCancel(schedule.Target, actorMember.RoleName, actorMember.ID, schedule.CreatedByMemberID) {
		return nil, authorization.ErrForbidden
	}
	now := time.Now().UTC()
	finishIndexMutation := s.beginIndexMutation()
	cancelledDeliveryIDs, err := s.repository.Cancel(ctx, spaceID, scheduleID, now)
	if err != nil {
		finishIndexMutation(err)
		return nil, err
	}
	schedule.Status, schedule.NextScheduledAt, schedule.UpdatedAt = domainscheduledmessage.ScheduleStatusCancelled, nil, now
	var indexErr error
	for _, deliveryID := range cancelledDeliveryIDs {
		indexErr = errors.Join(indexErr, s.removeFromIndex(ctx, deliveryID))
	}
	finishIndexMutation(indexErr)
	s.writeAudit(ctx, s.auditEvent(ctx, actor, domainaudit.ActionDelete, scheduledMessageAuditScope{spaceID: spaceID, resourceID: schedule.ID, ownerID: schedule.CreatedByMemberID}, domainaudit.StatusSuccess, map[string]any{"target": schedule.Target, "status": schedule.Status}))
	return schedule, nil
}

func (s *service) Unsubscribe(ctx context.Context, actor domainidentity.ActorContext, rawSpaceID, rawScheduleID string) (result *domainscheduledmessage.Schedule, err error) {
	spaceID := strings.TrimSpace(actor.SpaceID)
	scheduleID := ""
	defer func() {
		if err != nil {
			event := s.auditEvent(ctx, actor, "unsubscribe", scheduledMessageAuditScope{spaceID: spaceID, resourceID: scheduleID, ownerID: actor.MemberID}, domainaudit.StatusFailed, nil)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()
	var resolveErr error
	spaceID, resolveErr = resolveSpaceID(rawSpaceID, actor.SpaceID)
	if resolveErr != nil {
		return nil, resolveErr
	}
	scheduleID, err = parseUUID(rawScheduleID, "schedule_id")
	if err != nil {
		return nil, err
	}
	actorMember, _, err := s.authorizedMember(ctx, actor, spaceID, unsubscribePermission)
	if err != nil {
		return nil, err
	}
	schedule, err := s.findSchedule(ctx, spaceID, scheduleID)
	if err != nil {
		return nil, err
	}
	if !domainscheduledmessage.CanUnsubscribe(schedule.Target, actorMember.ID, memberID(schedule.RecipientMemberID)) {
		return nil, authorization.ErrForbidden
	}
	if schedule.Status == domainscheduledmessage.ScheduleStatusUnsubscribed {
		if schedule.UnsubscribedByMemberID != nil && *schedule.UnsubscribedByMemberID == actorMember.ID {
			return schedule, nil
		}
		return nil, interfacescheduledmessage.ErrConflict
	}
	if schedule.Status != domainscheduledmessage.ScheduleStatusActive {
		return nil, interfacescheduledmessage.ErrConflict
	}
	now := time.Now().UTC()
	finishIndexMutation := s.beginIndexMutation()
	cancelledDeliveryIDs, err := s.repository.Unsubscribe(ctx, spaceID, scheduleID, actorMember.ID, now)
	if err != nil {
		finishIndexMutation(err)
		if errors.Is(err, interfacescheduledmessage.ErrConflict) {
			latest, readErr := s.findSchedule(ctx, spaceID, scheduleID)
			if readErr == nil && latest.Status == domainscheduledmessage.ScheduleStatusUnsubscribed && latest.UnsubscribedByMemberID != nil && *latest.UnsubscribedByMemberID == actorMember.ID {
				return latest, nil
			}
		}
		return nil, err
	}
	schedule.Status, schedule.NextScheduledAt = domainscheduledmessage.ScheduleStatusUnsubscribed, nil
	schedule.UnsubscribedByMemberID, schedule.UnsubscribedAt, schedule.UpdatedAt = &actorMember.ID, &now, now
	var indexErr error
	for _, deliveryID := range cancelledDeliveryIDs {
		indexErr = errors.Join(indexErr, s.removeFromIndex(ctx, deliveryID))
	}
	finishIndexMutation(indexErr)
	s.writeAudit(ctx, s.auditEvent(ctx, actor, "unsubscribe", scheduledMessageAuditScope{spaceID: spaceID, resourceID: schedule.ID, ownerID: schedule.CreatedByMemberID}, domainaudit.StatusSuccess, map[string]any{"target": schedule.Target, "status": schedule.Status}))
	return schedule, nil
}

func (s *service) authorizedMember(ctx context.Context, actor domainidentity.ActorContext, spaceID, permission string) (*domainspace.ResolvedMembership, []domainspace.ResolvedMembership, error) {
	if s == nil || s.repository == nil || s.spaces == nil || s.authorize == nil {
		return nil, nil, errors.New("scheduled message service is not configured")
	}
	if err := s.authorize.Authorize(ctx, actor, permission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, nil, err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return nil, nil, err
	}
	member := activeActorMember(members, actor.MemberID, actor.UserID)
	if member == nil {
		return nil, nil, authorization.ErrNotFound
	}
	return member, members, nil
}

func (s *service) findSchedule(ctx context.Context, spaceID, scheduleID string) (*domainscheduledmessage.Schedule, error) {
	schedule, err := s.repository.FindByIDInSpace(ctx, spaceID, scheduleID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, authorization.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if schedule == nil || schedule.SpaceID != spaceID || schedule.ID != scheduleID {
		return nil, authorization.ErrNotFound
	}
	return schedule, nil
}

func (s *service) updateFields(schedule *domainscheduledmessage.Schedule, input dto.ScheduledMessageUpdateInput, now time.Time) (dto.ScheduledMessageUpdateInput, time.Time, domainscheduledmessage.ContentMode, string, domainscheduledmessage.Recurrence, string, time.Time, error) {
	fields := dto.ScheduledMessageUpdateInput{}
	contentMode, content, err := s.updateContentFields(schedule, input, &fields)
	if err != nil {
		return fields, time.Time{}, "", "", "", "", time.Time{}, err
	}
	recurrence, timezone, startAt, err := updateScheduleFields(input, &fields, schedule.Recurrence, schedule.Timezone, schedule.StartAt)
	if err != nil {
		return fields, time.Time{}, "", "", "", "", time.Time{}, err
	}
	if fields.ContentMode == nil && fields.Content == nil && fields.Recurrence == nil && fields.Timezone == nil && fields.StartAt == nil {
		return fields, time.Time{}, "", "", "", "", time.Time{}, invalid("update", "at least one field is required")
	}
	if fields.Recurrence != nil || fields.Timezone != nil || fields.StartAt != nil {
		if err := validateRecurrence(recurrence, startAt, now, timezone); err != nil {
			return fields, time.Time{}, "", "", "", "", time.Time{}, err
		}
	}
	nextAt, err := updatedNextScheduledAt(schedule, recurrence, startAt, timezone, now, fields.StartAt != nil)
	if err != nil {
		return fields, time.Time{}, "", "", "", "", time.Time{}, err
	}
	return fields, nextAt, contentMode, content, recurrence, timezone, startAt, nil
}

func (s *service) updateContentFields(schedule *domainscheduledmessage.Schedule, input dto.ScheduledMessageUpdateInput, fields *dto.ScheduledMessageUpdateInput) (domainscheduledmessage.ContentMode, string, error) {
	contentMode, content := schedule.ContentMode, schedule.Content
	if input.ContentMode != nil {
		if *input.ContentMode != domainscheduledmessage.ContentModeFixed && *input.ContentMode != domainscheduledmessage.ContentModeAI {
			return "", "", invalid("content_mode", "must be FIXED or AI")
		}
		contentMode = *input.ContentMode
		fields.ContentMode = &contentMode
	}
	if input.Content != nil {
		var err error
		content, err = validateContent(*input.Content, s.maxTextBytes)
		if err != nil {
			return "", "", err
		}
		fields.Content = &content
	}
	if fields.ContentMode != nil || fields.Content != nil {
		if contentMode == domainscheduledmessage.ContentModeAI && s.generator == nil {
			return "", "", invalid("content_mode", "AI generation is unavailable")
		}
		if content == "" || len(content) > s.maxTextBytes {
			return "", "", invalid("content", fmt.Sprintf("must contain 1 to %d bytes", s.maxTextBytes))
		}
	}
	return contentMode, content, nil
}

func updateScheduleFields(input dto.ScheduledMessageUpdateInput, fields *dto.ScheduledMessageUpdateInput, recurrence domainscheduledmessage.Recurrence, timezone string, startAt time.Time) (domainscheduledmessage.Recurrence, string, time.Time, error) {
	if input.Recurrence != nil {
		if !validRecurrence(*input.Recurrence) {
			return "", "", time.Time{}, invalid("recurrence", invalidValueReason)
		}
		recurrence = *input.Recurrence
		fields.Recurrence = &recurrence
	}
	if input.Timezone != nil {
		var err error
		timezone, err = validateTimezone(*input.Timezone)
		if err != nil {
			return "", "", time.Time{}, err
		}
		fields.Timezone = &timezone
	}
	if input.StartAt != nil {
		var err error
		startAt, err = validateFutureTime(*input.StartAt)
		if err != nil {
			return "", "", time.Time{}, err
		}
		fields.StartAt = &startAt
	}
	return recurrence, timezone, startAt, nil
}

func updatedNextScheduledAt(schedule *domainscheduledmessage.Schedule, recurrence domainscheduledmessage.Recurrence, startAt time.Time, timezone string, now time.Time, startAtChanged bool) (time.Time, error) {
	if schedule.NextScheduledAt == nil {
		return time.Time{}, interfacescheduledmessage.ErrConflict
	}
	nextAt := *schedule.NextScheduledAt
	if recurrence == domainscheduledmessage.RecurrenceOnce && startAtChanged {
		nextAt = startAt
	} else if recurrence != domainscheduledmessage.RecurrenceOnce {
		next, err := domainscheduledmessage.NextOccurrence(recurrence, startAt, now, timezone)
		if err != nil {
			return time.Time{}, recurrenceError(err)
		}
		if next == nil {
			return time.Time{}, interfacescheduledmessage.ErrConflict
		}
		nextAt = *next
	}
	return nextAt, nil
}

func activeActorMember(members []domainspace.ResolvedMembership, memberID, userID string) *domainspace.ResolvedMembership {
	memberID, userID = strings.TrimSpace(memberID), strings.TrimSpace(userID)
	for i := range members {
		member := &members[i]
		if strings.TrimSpace(member.Status) != domainspace.StatusActive {
			continue
		}
		if (memberID != "" && strings.TrimSpace(member.ID) == memberID) || (memberID == "" && userID != "" && strings.TrimSpace(member.UserID) == userID) {
			return member
		}
	}
	return nil
}

func memberByID(members []domainspace.ResolvedMembership, memberID string) *domainspace.ResolvedMembership {
	memberID = strings.TrimSpace(memberID)
	for i := range members {
		if strings.TrimSpace(members[i].ID) == memberID && strings.TrimSpace(members[i].Status) == domainspace.StatusActive {
			return &members[i]
		}
	}
	return nil
}

func resolveSpaceID(value, actorSpaceID string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(actorSpaceID)
	}
	return parseUUID(value, "space_id")
}

func parseUUID(value, field string) (string, error) {
	parsed, err := utils.ParseUUID(value, field, true)
	if err != nil {
		return "", invalid(field, "must be a valid UUID")
	}
	return parsed, nil
}

func validateContent(value string, maxBytes int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxBytes {
		return "", invalid("content", fmt.Sprintf("must contain 1 to %d bytes", maxBytes))
	}
	return value, nil
}

func validateFutureTime(value time.Time) (time.Time, error) {
	if value.IsZero() {
		return time.Time{}, invalid("start_at", "is required")
	}
	value = value.UTC()
	if !value.After(time.Now().UTC()) {
		return time.Time{}, invalid("start_at", "must be in the future")
	}
	return value, nil
}

func validateTimezone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "Local" || len(value) > 64 {
		return "", invalid("timezone", validIANATimezoneReason)
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", invalid("timezone", validIANATimezoneReason)
	}
	return value, nil
}

func validRecurrence(value domainscheduledmessage.Recurrence) bool {
	return value == domainscheduledmessage.RecurrenceOnce || value == domainscheduledmessage.RecurrenceDaily || value == domainscheduledmessage.RecurrenceWeekly
}

func validateRecurrence(value domainscheduledmessage.Recurrence, startAt, after time.Time, timezone string) error {
	if !validRecurrence(value) {
		return invalid("recurrence", invalidValueReason)
	}
	_, err := domainscheduledmessage.NextOccurrence(value, startAt, after, timezone)
	if err != nil {
		return recurrenceError(err)
	}
	return nil
}

func recurrenceError(err error) error {
	switch {
	case errors.Is(err, domainscheduledmessage.ErrInvalidTimezone):
		return invalid("timezone", validIANATimezoneReason)
	case errors.Is(err, domainscheduledmessage.ErrInvalidRecurrence):
		return invalid("recurrence", invalidValueReason)
	case errors.Is(err, domainscheduledmessage.ErrAmbiguousLocalTime), errors.Is(err, domainscheduledmessage.ErrNonexistentLocalTime):
		return invalid("start_at", "is not a valid local time in the selected timezone")
	default:
		return fmt.Errorf("validate recurrence: %w", err)
	}
}

func validScheduleStatus(value domainscheduledmessage.ScheduleStatus) bool {
	switch value {
	case domainscheduledmessage.ScheduleStatusActive, domainscheduledmessage.ScheduleStatusSent, domainscheduledmessage.ScheduleStatusFailed,
		domainscheduledmessage.ScheduleStatusCancelled, domainscheduledmessage.ScheduleStatusUnsubscribed:
		return true
	default:
		return false
	}
}

func isOwnerOrAdmin(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "space_owner", "space_admin":
		return true
	default:
		return false
	}
}

func invalid(field, reason string) error {
	return &authorization.ValidationError{Field: field, Reason: reason}
}

func memberID(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func scheduleID(value *domainscheduledmessage.Schedule) string {
	if value == nil {
		return ""
	}
	return value.ID
}

func scheduledMessageOutput(schedule *domainscheduledmessage.Schedule) dto.ScheduledMessageOutput {
	return dto.ScheduledMessageOutput{
		ID: schedule.ID, SpaceID: schedule.SpaceID, CreatedByMemberID: schedule.CreatedByMemberID,
		Target: schedule.Target, RecipientMemberID: schedule.RecipientMemberID, SpaceChannelID: schedule.SpaceChannelID,
		ContentMode: schedule.ContentMode, Content: schedule.Content, Recurrence: schedule.Recurrence, Timezone: schedule.Timezone,
		StartAt: schedule.StartAt, NextScheduledAt: schedule.NextScheduledAt, Status: schedule.Status,
		UnsubscribedByMemberID: schedule.UnsubscribedByMemberID, UnsubscribedAt: schedule.UnsubscribedAt,
		CreatedAt: schedule.CreatedAt, UpdatedAt: schedule.UpdatedAt,
	}
}

func deliveryOutput(delivery *domainscheduledmessage.Delivery) dto.ScheduledMessageDeliveryOutput {
	return dto.ScheduledMessageDeliveryOutput{
		ID: delivery.ID, ScheduledMessageID: delivery.ScheduledMessageID, ScheduledAt: delivery.ScheduledAt,
		ContentSnapshot: delivery.ContentSnapshot, GenerationAttempts: delivery.GenerationAttempts,
		DeliveryAttempts: delivery.DeliveryAttempts, NextAttemptAt: delivery.NextAttemptAt, Status: delivery.Status,
		FailureCategory: delivery.FailureCategory, SentAt: delivery.SentAt,
	}
}

// Due-index keys are immutable delivery UUIDs; schedule UUIDs are never index keys.
func (s *service) beginIndexMutation() func(error) {
	if s.index == nil {
		return func(error) {
			// No index means there is no lifecycle mutation to finish.
		}
	}
	s.index.BeginLifecycleMutation()
	return s.index.FinishLifecycleMutation
}

func (s *service) upsertIndex(ctx context.Context, deliveryID string, at time.Time) error {
	if s.index != nil {
		if err := s.index.Upsert(ctx, deliveryID, at); err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message due index upsert failed")
			return err
		}
	}
	return nil
}

func (s *service) removeFromIndex(ctx context.Context, deliveryID string) error {
	if s.index != nil {
		if err := s.index.Remove(ctx, deliveryID); err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message due index removal failed")
			return err
		}
	}
	return nil
}

func (s *service) auditEvent(ctx context.Context, actor domainidentity.ActorContext, action string, scope scheduledMessageAuditScope, status string, metadata map[string]any) domainaudit.AuditEvent {
	if metadata == nil {
		metadata = make(map[string]any, 2)
	}
	metadata["space_id"] = strings.TrimSpace(scope.spaceID)
	actorUserID, actorRole := strings.TrimSpace(actor.UserID), strings.TrimSpace(actor.RoleName)
	if initiatorID := strings.TrimSpace(actor.InitiatorUserID); initiatorID != "" {
		actorUserID = initiatorID
		if subjectID := strings.TrimSpace(actor.UserID); subjectID != "" && subjectID != initiatorID {
			metadata["subject_user_id"] = subjectID
		}
	}
	if role := strings.TrimSpace(actor.InitiatorRoleName); role != "" {
		actorRole = role
	}
	return domainaudit.AuditEvent{
		ActorUserID: actorUserID, ActorMemberID: strings.TrimSpace(actor.MemberID),
		ResourceOwnerMemberID: strings.TrimSpace(scope.ownerID), Source: auditSource(actor.Source),
		Channel: safeAuditChannel(actor.Channel), ActorRole: actorRole, Action: action,
		Resource: "scheduled_message", ResourceID: strings.TrimSpace(scope.resourceID), Status: status,
		RequestID: logger.LogIDFromContext(ctx), Metadata: metadata,
	}
}

func (s *service) writeAudit(ctx context.Context, event domainaudit.AuditEvent) {
	if s != nil && s.audit != nil {
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

func safeAuditChannel(channel string) string {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if len(channel) > 32 {
		return "unknown"
	}
	return channel
}

func failureCategory(err error) string {
	var validationErr *authorization.ValidationError
	switch {
	case errors.As(err, &validationErr), errors.Is(err, authorization.ErrInvalidResource):
		return "validation"
	case errors.Is(err, authorization.ErrForbidden):
		return "forbidden"
	case errors.Is(err, authorization.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "not_found"
	case errors.Is(err, interfacescheduledmessage.ErrConflict), errors.Is(err, interfacescheduledmessage.ErrInvalidWhatsAppIdentity):
		return "conflict"
	default:
		return "internal"
	}
}

var _ interfacescheduledmessage.Service = (*service)(nil)
