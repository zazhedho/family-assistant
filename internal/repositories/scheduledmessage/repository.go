package repositoryscheduledmessage

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	"family-assistant/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	spaceActiveStatus   = "ACTIVE"
	memberActiveStatus  = "ACTIVE"
	maxScheduleListRows = 100
	whatsappChannel     = "whatsapp"
	groupChatType       = "group"
	groupChatIdentifier = `^[0-9]*[1-9][0-9]*@g[.]us$`
	whatsappIdentifier  = `^[0-9]*[1-9][0-9]*@(s[.]whatsapp[.]net|lid)$`
)

var (
	groupChatPattern        = regexp.MustCompile(groupChatIdentifier)
	whatsappIdentityPattern = regexp.MustCompile(whatsappIdentifier)
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{DB: db} }

func NewSchedulerRepository(db *gorm.DB) *Repository { return &Repository{DB: db} }

var _ interfacescheduledmessage.Repository = (*Repository)(nil)
var _ interfacescheduledmessage.SchedulerRepository = (*Repository)(nil)

func (r *Repository) Create(ctx context.Context, schedule *domainscheduledmessage.Schedule, first *domainscheduledmessage.Delivery) error {
	if schedule == nil || first == nil || !first.ScheduledAt.Equal(schedule.StartAt) {
		return interfacescheduledmessage.ErrConflict
	}
	now := time.Now().UTC()
	setCreationDefaults(schedule, first, now)
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(schedule).Error; err != nil {
			return err
		}
		return tx.Create(first).Error
	})
}

func setCreationDefaults(schedule *domainscheduledmessage.Schedule, first *domainscheduledmessage.Delivery, now time.Time) {
	if strings.TrimSpace(schedule.ID) == "" {
		schedule.ID = utils.CreateUUID()
	}
	if strings.TrimSpace(first.ID) == "" {
		first.ID = utils.CreateUUID()
	}
	first.ScheduledMessageID = schedule.ID
	if first.Status == "" {
		first.Status = domainscheduledmessage.DeliveryStatusPending
	}
	if first.NextAttemptAt == nil {
		first.NextAttemptAt = &first.ScheduledAt
	}
	if schedule.NextScheduledAt == nil {
		schedule.NextScheduledAt = &schedule.StartAt
	}
	if schedule.Status == "" {
		schedule.Status = domainscheduledmessage.ScheduleStatusActive
	}
	if first.Status == domainscheduledmessage.DeliveryStatusPending && schedule.ContentMode == domainscheduledmessage.ContentModeFixed && first.ContentSnapshot == nil {
		content := schedule.Content
		first.ContentSnapshot = &content
	}
	setCreatedAt(&schedule.CreatedAt, &schedule.UpdatedAt, now)
	setCreatedAt(&first.CreatedAt, &first.UpdatedAt, now)
}

func setCreatedAt(createdAt, updatedAt *time.Time, now time.Time) {
	if createdAt.IsZero() {
		*createdAt = now
	}
	if updatedAt.IsZero() {
		*updatedAt = now
	}
}

func (r *Repository) FindByIDInSpace(ctx context.Context, spaceID, scheduleID string) (*domainscheduledmessage.Schedule, error) {
	var schedule domainscheduledmessage.Schedule
	err := r.DB.WithContext(ctx).Model(&domainscheduledmessage.Schedule{}).
		Joins("JOIN spaces ON spaces.id = scheduled_messages.space_id").
		Where("scheduled_messages.space_id = ? AND scheduled_messages.id = ? AND spaces.status = ? AND spaces.deleted_at IS NULL", spaceID, scheduleID, spaceActiveStatus).
		First(&schedule).Error
	return &schedule, err
}

func (r *Repository) List(ctx context.Context, input dto.ScheduledMessageListInput) ([]domainscheduledmessage.Schedule, error) {
	query := r.DB.WithContext(ctx).Model(&domainscheduledmessage.Schedule{}).
		Joins("JOIN spaces ON spaces.id = scheduled_messages.space_id").
		Where("scheduled_messages.space_id = ? AND spaces.status = ? AND spaces.deleted_at IS NULL", strings.TrimSpace(input.Space), spaceActiveStatus)
	if input.ScheduleID != nil {
		query = query.Where("scheduled_messages.id = ?", strings.TrimSpace(*input.ScheduleID))
	}
	if input.Status != nil {
		query = query.Where("scheduled_messages.status = ?", *input.Status)
	}
	query = query.Where("(scheduled_messages.target <> ? OR ? OR scheduled_messages.created_by_member_id = ? OR scheduled_messages.recipient_member_id = ?)",
		domainscheduledmessage.TargetDM, input.CanViewAllDMs, strings.TrimSpace(input.ViewerMemberID), strings.TrimSpace(input.ViewerMemberID))
	var schedules []domainscheduledmessage.Schedule
	err := query.Order("scheduled_messages.start_at ASC, scheduled_messages.id ASC").Limit(boundedScheduleListLimit(input.Limit)).Find(&schedules).Error
	return schedules, err
}

func boundedScheduleListLimit(limit int) int {
	if limit < 1 || limit > maxScheduleListRows {
		return maxScheduleListRows
	}
	return limit
}

func (r *Repository) ListHistory(ctx context.Context, spaceID, scheduleID string, limit int) ([]domainscheduledmessage.Delivery, error) {
	var history []domainscheduledmessage.Delivery
	err := r.DB.WithContext(ctx).Model(&domainscheduledmessage.Delivery{}).
		Joins("JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id").
		Joins("JOIN spaces ON spaces.id = scheduled_messages.space_id").
		Where("scheduled_messages.space_id = ? AND scheduled_messages.id = ? AND spaces.status = ? AND spaces.deleted_at IS NULL", spaceID, scheduleID, spaceActiveStatus).
		Order("scheduled_message_deliveries.scheduled_at DESC, scheduled_message_deliveries.id DESC").Limit(boundedHistoryLimit(limit)).Find(&history).Error
	return history, err
}

func boundedHistoryLimit(limit int) int {
	if limit < 1 || limit > maxScheduleListRows {
		return maxScheduleListRows
	}
	return limit
}

func (r *Repository) UpdateActive(ctx context.Context, spaceID, scheduleID string, fields dto.ScheduledMessageUpdateInput, next *domainscheduledmessage.Delivery, updatedAt time.Time) ([]string, error) {
	if needsOccurrenceReplacement(fields) && next == nil {
		return nil, interfacescheduledmessage.ErrConflict
	}
	var cancelledDeliveryIDs []string
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		schedule, err := lockSchedule(tx, spaceID, scheduleID)
		if err != nil {
			return err
		}
		updates := scheduleUpdates(fields, updatedAt)
		if next != nil {
			cancelledDeliveryIDs, err = replacePendingOccurrence(tx, schedule.ID, next, updatedAt)
			if err != nil {
				return err
			}
			updates["next_scheduled_at"] = next.ScheduledAt
		}
		if len(updates) == 1 {
			return interfacescheduledmessage.ErrConflict
		}
		return updateSchedule(tx, schedule.ID, updates)
	})
	if err != nil {
		return nil, err
	}
	return cancelledDeliveryIDs, nil
}

func needsOccurrenceReplacement(fields dto.ScheduledMessageUpdateInput) bool {
	return fields.ContentMode != nil || fields.Content != nil || fields.Recurrence != nil || fields.Timezone != nil || fields.StartAt != nil
}

func scheduleUpdates(fields dto.ScheduledMessageUpdateInput, updatedAt time.Time) map[string]any {
	updates := map[string]any{"updated_at": updatedAt}
	if fields.ContentMode != nil {
		updates["content_mode"] = *fields.ContentMode
	}
	if fields.Content != nil {
		updates["content"] = *fields.Content
	}
	if fields.Recurrence != nil {
		updates["recurrence"] = *fields.Recurrence
	}
	if fields.Timezone != nil {
		updates["timezone"] = *fields.Timezone
	}
	if fields.StartAt != nil {
		updates["start_at"] = *fields.StartAt
	}
	return updates
}

func replacePendingOccurrence(tx *gorm.DB, scheduleID string, next *domainscheduledmessage.Delivery, updatedAt time.Time) ([]string, error) {
	var pending []domainscheduledmessage.Delivery
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("scheduled_message_id = ? AND status = ?", scheduleID, domainscheduledmessage.DeliveryStatusPending).
		Order("scheduled_at ASC, id ASC").Find(&pending).Error; err != nil {
		return nil, err
	}

	var existing domainscheduledmessage.Delivery
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("scheduled_message_id = ? AND scheduled_at = ?", scheduleID, next.ScheduledAt).
		Take(&existing).Error
	existingFound := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existingFound && existing.Status != domainscheduledmessage.DeliveryStatusPending && existing.Status != domainscheduledmessage.DeliveryStatusCancelled {
		return nil, interfacescheduledmessage.ErrConflict
	}

	cancelledDeliveryIDs := make([]string, 0, len(pending))
	for i := range pending {
		cancelledDeliveryIDs = append(cancelledDeliveryIDs, pending[i].ID)
		if existingFound && pending[i].ID == existing.ID {
			continue
		}
		if err := tx.Model(&domainscheduledmessage.Delivery{}).Where("id = ? AND status = ?", pending[i].ID, domainscheduledmessage.DeliveryStatusPending).
			Updates(map[string]any{"status": domainscheduledmessage.DeliveryStatusCancelled, "lease_token": nil, "lease_until": nil, "updated_at": updatedAt}).Error; err != nil {
			return nil, err
		}
	}

	if strings.TrimSpace(next.ID) == "" {
		next.ID = utils.CreateUUID()
	}
	next.ScheduledMessageID = scheduleID
	next.Status = domainscheduledmessage.DeliveryStatusPending
	if existingFound {
		next.ID = existing.ID
		next.ScheduledAt = existing.ScheduledAt
		next.GenerationAttempts = existing.GenerationAttempts
		next.DeliveryAttempts = existing.DeliveryAttempts
		next.CreatedAt = existing.CreatedAt
	}
	next.NextAttemptAt = &next.ScheduledAt
	setCreatedAt(&next.CreatedAt, &next.UpdatedAt, updatedAt)
	if existingFound {
		if err := tx.Model(&domainscheduledmessage.Delivery{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"content_snapshot": next.ContentSnapshot,
			"next_attempt_at":  next.NextAttemptAt,
			"lease_token":      nil,
			"lease_until":      nil,
			"status":           domainscheduledmessage.DeliveryStatusPending,
			"failure_category": nil,
			"sent_at":          nil,
			"updated_at":       updatedAt,
		}).Error; err != nil {
			return nil, err
		}
		return cancelledDeliveryIDs, nil
	}
	if err := tx.Create(next).Error; err != nil {
		return nil, err
	}
	return cancelledDeliveryIDs, nil
}

func (r *Repository) Cancel(ctx context.Context, spaceID, scheduleID string, cancelledAt time.Time) ([]string, error) {
	return r.transitionTerminal(ctx, spaceID, scheduleID, nil, domainscheduledmessage.ScheduleStatusCancelled, cancelledAt)
}

func (r *Repository) Unsubscribe(ctx context.Context, spaceID, scheduleID, memberID string, at time.Time) ([]string, error) {
	return r.transitionTerminal(ctx, spaceID, scheduleID, &memberID, domainscheduledmessage.ScheduleStatusUnsubscribed, at)
}

func (r *Repository) transitionTerminal(ctx context.Context, spaceID, scheduleID string, unsubscribedBy *string, status domainscheduledmessage.ScheduleStatus, at time.Time) ([]string, error) {
	var cancelledDeliveryIDs []string
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		schedule, err := lockSchedule(tx, spaceID, scheduleID)
		if err != nil {
			return err
		}
		var deliveries []domainscheduledmessage.Delivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scheduled_message_id = ? AND status = ?", schedule.ID, domainscheduledmessage.DeliveryStatusPending).
			Order("scheduled_at ASC, id ASC").Find(&deliveries).Error; err != nil {
			return err
		}
		cancelledDeliveryIDs = make([]string, 0, len(deliveries))
		for i := range deliveries {
			cancelledDeliveryIDs = append(cancelledDeliveryIDs, deliveries[i].ID)
		}
		if len(deliveries) > 0 {
			if err := tx.Model(&domainscheduledmessage.Delivery{}).
				Where("scheduled_message_id = ? AND status = ?", schedule.ID, domainscheduledmessage.DeliveryStatusPending).
				Updates(map[string]any{"status": domainscheduledmessage.DeliveryStatusCancelled, "lease_token": nil, "lease_until": nil, "updated_at": at}).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"status": status, "next_scheduled_at": nil, "updated_at": at}
		if unsubscribedBy != nil {
			updates["unsubscribed_by_member_id"] = *unsubscribedBy
			updates["unsubscribed_at"] = at
		}
		return updateSchedule(tx, schedule.ID, updates)
	})
	if err != nil {
		return nil, err
	}
	return cancelledDeliveryIDs, nil
}

func lockSchedule(tx *gorm.DB, spaceID, scheduleID string) (*domainscheduledmessage.Schedule, error) {
	var schedule domainscheduledmessage.Schedule
	err := tx.Model(&domainscheduledmessage.Schedule{}).
		Joins("JOIN spaces ON spaces.id = scheduled_messages.space_id").
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "scheduled_messages"}}).
		Where("scheduled_messages.space_id = ? AND scheduled_messages.id = ? AND scheduled_messages.status = ? AND spaces.status = ? AND spaces.deleted_at IS NULL",
			spaceID, scheduleID, domainscheduledmessage.ScheduleStatusActive, spaceActiveStatus).
		First(&schedule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfacescheduledmessage.ErrConflict
	}
	return &schedule, err
}

func updateSchedule(tx *gorm.DB, id string, updates map[string]any) error {
	result := tx.Model(&domainscheduledmessage.Schedule{}).Where("id = ? AND status = ?", id, domainscheduledmessage.ScheduleStatusActive).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return interfacescheduledmessage.ErrConflict
	}
	return nil
}

func (r *Repository) BindCurrentGroup(ctx context.Context, spaceID, actorMemberID, channel, chatID, chatType string, at time.Time) (*domainscheduledmessage.SpaceChannel, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	chatID = strings.TrimSpace(chatID)
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	if channel != whatsappChannel || chatType != groupChatType || !groupChatPattern.MatchString(chatID) {
		return nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	var bound domainscheduledmessage.SpaceChannel
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := verifySpaceAdmin(tx, spaceID, actorMemberID); err != nil {
			return err
		}
		var existing domainscheduledmessage.SpaceChannel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("external_chat_id = ? AND status = ?", chatID, domainscheduledmessage.SpaceChannelStatusActive).First(&existing).Error
		if err == nil {
			if existing.SpaceID != spaceID || existing.Channel != channel {
				return interfacescheduledmessage.ErrConflict
			}
			if err := tx.Model(&domainscheduledmessage.SpaceChannel{}).Where("id = ?", existing.ID).
				Updates(map[string]any{"bound_by_member_id": actorMemberID, "updated_at": at}).Error; err != nil {
				return err
			}
			existing.BoundByMemberID = actorMemberID
			existing.UpdatedAt = at
			bound = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		bound = domainscheduledmessage.SpaceChannel{
			ID: utils.CreateUUID(), SpaceID: spaceID, Channel: channel, ExternalChatID: chatID,
			Status: domainscheduledmessage.SpaceChannelStatusActive, BoundByMemberID: actorMemberID, CreatedAt: at, UpdatedAt: at,
		}
		return tx.Create(&bound).Error
	})
	return &bound, err
}

func verifySpaceAdmin(tx *gorm.DB, spaceID, memberID string) error {
	var count int64
	err := tx.Table("space_members").Joins("JOIN roles ON roles.id = space_members.role_id").
		Joins("JOIN spaces ON spaces.id = space_members.space_id").
		Where("space_members.id = ? AND space_members.space_id = ? AND space_members.status = ? AND space_members.deleted_at IS NULL AND spaces.status = ? AND spaces.deleted_at IS NULL AND roles.deleted_at IS NULL AND roles.name IN ?",
			memberID, spaceID, memberActiveStatus, spaceActiveStatus, []string{"space_owner", "space_admin"}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return interfacescheduledmessage.ErrConflict
	}
	return nil
}

func (r *Repository) ListTargets(ctx context.Context, spaceID string) ([]dto.ScheduledMessageTargetOutput, error) {
	var targets []dto.ScheduledMessageTargetOutput
	err := r.DB.WithContext(ctx).Raw(`
SELECT 'DM' AS kind, m.id AS member_id, NULL::uuid AS space_channel_id, u.name AS display_name
FROM space_members m
JOIN spaces s ON s.id = m.space_id AND s.status = 'ACTIVE' AND s.deleted_at IS NULL
JOIN users u ON u.id = m.user_id
WHERE m.space_id = ? AND m.status = 'ACTIVE' AND m.deleted_at IS NULL
  AND EXISTS (SELECT 1 FROM external_identities ei WHERE ei.user_id = m.user_id AND ei.provider = 'hermes'
      AND ei.status = 'ACTIVE' AND ei.deleted_at IS NULL AND ei.verified_at IS NOT NULL
      AND ei.external_id ~ ?)
UNION ALL
SELECT 'GROUP' AS kind, NULL::uuid AS member_id, c.id AS space_channel_id, 'WhatsApp group' AS display_name
FROM space_channels c
JOIN spaces s ON s.id = c.space_id AND s.status = 'ACTIVE' AND s.deleted_at IS NULL
WHERE c.space_id = ? AND c.status = 'ACTIVE' AND c.channel = 'whatsapp'
  AND c.external_chat_id ~ '^[0-9]*[1-9][0-9]*@g[.]us$'
ORDER BY kind, display_name`, spaceID, whatsappIdentifier, spaceID).Scan(&targets).Error
	return targets, err
}
