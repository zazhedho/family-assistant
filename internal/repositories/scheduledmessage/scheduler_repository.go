package repositoryscheduledmessage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	domainspace "family-assistant/internal/domain/space"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	"family-assistant/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	scheduledMessagesResource = "scheduled_messages"
	scheduledMessagesCreate   = "create"
)

func (r *Repository) ListPendingForIndex(ctx context.Context) ([]domainscheduledmessage.Delivery, error) {
	var deliveries []domainscheduledmessage.Delivery
	err := r.DB.WithContext(ctx).Model(&domainscheduledmessage.Delivery{}).
		Joins("JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id").
		Joins("JOIN spaces ON spaces.id = scheduled_messages.space_id").
		Where("scheduled_message_deliveries.status = ? AND scheduled_messages.status = ? AND spaces.status = ? AND spaces.deleted_at IS NULL",
			domainscheduledmessage.DeliveryStatusPending, domainscheduledmessage.ScheduleStatusActive, spaceActiveStatus).
		Order("scheduled_message_deliveries.next_attempt_at ASC, scheduled_message_deliveries.id ASC").Find(&deliveries).Error
	return deliveries, err
}

func (r *Repository) ListGeneratedHistory(ctx context.Context, spaceID, scheduleID string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	if limit > 5 {
		limit = 5
	}
	var messages []string
	err := r.DB.WithContext(ctx).Raw(`
SELECT d.content_snapshot
FROM scheduled_message_deliveries d
JOIN scheduled_messages s ON s.id = d.scheduled_message_id
JOIN spaces p ON p.id = s.space_id
WHERE s.content_mode = ? AND d.status = ? AND p.status = ? AND p.deleted_at IS NULL
  AND d.content_snapshot IS NOT NULL AND s.space_id = ? AND s.id = ?
ORDER BY d.scheduled_at DESC, d.id DESC
LIMIT ?`, domainscheduledmessage.ContentModeAI, domainscheduledmessage.DeliveryStatusSent, spaceActiveStatus, spaceID, scheduleID, limit).Scan(&messages).Error
	return messages, err
}

func (r *Repository) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]domainscheduledmessage.Delivery, error) {
	if limit <= 0 {
		limit = 50
	}
	leaseUntil := now.Add(lease)
	claimed := make([]domainscheduledmessage.Delivery, 0, limit)
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scheduleIDs, err := lockDueScheduleIDs(tx, now, limit)
		if err != nil || len(scheduleIDs) == 0 {
			return err
		}
		var due []domainscheduledmessage.Delivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("scheduled_message_id IN ? AND status = ? AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", scheduleIDs, domainscheduledmessage.DeliveryStatusPending, now, now).
			Order("next_attempt_at ASC, id ASC").Limit(limit).Find(&due).Error; err != nil {
			return err
		}
		for i := range due {
			if err := claimDelivery(tx, &due[i], now, leaseUntil); err != nil {
				return err
			}
			claimed = append(claimed, due[i])
		}
		return nil
	})
	return claimed, err
}

func lockDueScheduleIDs(tx *gorm.DB, now time.Time, limit int) ([]string, error) {
	var ids []string
	err := tx.Raw(`SELECT s.id FROM scheduled_messages s
JOIN spaces p ON p.id = s.space_id
JOIN scheduled_message_deliveries d ON d.scheduled_message_id = s.id
WHERE s.status = ? AND p.status = ? AND p.deleted_at IS NULL
  AND d.status = ? AND d.next_attempt_at <= ? AND (d.lease_until IS NULL OR d.lease_until <= ?)
ORDER BY d.next_attempt_at ASC, s.id ASC
LIMIT ? FOR UPDATE OF s SKIP LOCKED`, domainscheduledmessage.ScheduleStatusActive, spaceActiveStatus,
		domainscheduledmessage.DeliveryStatusPending, now, now, limit).Scan(&ids).Error
	return ids, err
}

func claimDelivery(tx *gorm.DB, delivery *domainscheduledmessage.Delivery, now, leaseUntil time.Time) error {
	token := utils.CreateUUID()
	result := tx.Model(&domainscheduledmessage.Delivery{}).
		Where(`id = ? AND status = ? AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?)
AND EXISTS (SELECT 1 FROM scheduled_messages s JOIN spaces p ON p.id = s.space_id
WHERE s.id = scheduled_message_deliveries.scheduled_message_id AND s.status = ? AND p.status = ? AND p.deleted_at IS NULL)`,
			delivery.ID, domainscheduledmessage.DeliveryStatusPending, now, now, domainscheduledmessage.ScheduleStatusActive, spaceActiveStatus).
		Updates(map[string]any{"lease_token": token, "lease_until": leaseUntil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return interfacescheduledmessage.ErrClaimConflict
	}
	delivery.LeaseToken = &token
	delivery.LeaseExpiresAt = &leaseUntil
	delivery.UpdatedAt = now
	return nil
}

func (r *Repository) LoadClaimed(ctx context.Context, deliveryID, token string, now time.Time) (*domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity, string, error) {
	delivery, schedule, err := r.findClaimed(ctx, deliveryID, token, now)
	if err != nil {
		if errors.Is(err, interfacescheduledmessage.ErrInvalidTarget) {
			return schedule, delivery, nil, nil, "", err
		}
		return nil, nil, nil, nil, "", err
	}
	if err := r.hasCurrentCreatePermission(ctx, schedule); err != nil {
		return schedule, delivery, nil, nil, "", err
	}
	channel, identity, name, err := r.loadTarget(ctx, schedule)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, interfacescheduledmessage.ErrInvalidWhatsAppIdentity) {
		return schedule, delivery, nil, nil, "", interfacescheduledmessage.ErrInvalidTarget
	}
	return schedule, delivery, channel, identity, name, err
}

func (r *Repository) hasCurrentCreatePermission(ctx context.Context, schedule *domainscheduledmessage.Schedule) error {
	if schedule == nil {
		return interfacescheduledmessage.ErrInvalidTarget
	}
	var result struct {
		Allowed bool `gorm:"column:allowed"`
	}
	err := r.DB.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM space_members m
JOIN roles r ON r.id = m.role_id
JOIN role_permissions rp ON rp.role_id = r.id
JOIN permissions p ON p.id = rp.permission_id
WHERE m.id = ? AND m.space_id = ? AND m.status = ? AND m.deleted_at IS NULL
  AND r.deleted_at IS NULL AND p.deleted_at IS NULL AND p.resource = ? AND p.action = ?) AS allowed`,
		schedule.CreatedByMemberID, schedule.SpaceID, memberActiveStatus, scheduledMessagesResource, scheduledMessagesCreate).Scan(&result).Error
	if err != nil {
		return err
	}
	if !result.Allowed {
		return interfacescheduledmessage.ErrInvalidTarget
	}
	return nil
}

func (r *Repository) findClaimed(ctx context.Context, deliveryID, token string, now time.Time) (*domainscheduledmessage.Delivery, *domainscheduledmessage.Schedule, error) {
	var delivery domainscheduledmessage.Delivery
	err := r.DB.WithContext(ctx).Where("id = ? AND status = ? AND lease_token = ? AND lease_until > ?", deliveryID, domainscheduledmessage.DeliveryStatusPending, token, now).First(&delivery).Error
	if err != nil {
		return nil, nil, err
	}
	var schedule domainscheduledmessage.Schedule
	err = r.DB.WithContext(ctx).Model(&domainscheduledmessage.Schedule{}).
		Where("scheduled_messages.id = ? AND scheduled_messages.status = ?", delivery.ScheduledMessageID, domainscheduledmessage.ScheduleStatusActive).
		First(&schedule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, interfacescheduledmessage.ErrClaimConflict
	}
	if err != nil {
		return nil, nil, err
	}
	var space struct{ Status string }
	err = r.DB.WithContext(ctx).Table("spaces").Select("status").Where("id = ? AND deleted_at IS NULL", schedule.SpaceID).Take(&space).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && space.Status != spaceActiveStatus {
		return &delivery, &schedule, interfacescheduledmessage.ErrInvalidTarget
	}
	return &delivery, &schedule, err
}

func (r *Repository) loadTarget(ctx context.Context, schedule *domainscheduledmessage.Schedule) (*domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity, string, error) {
	if schedule.Target == domainscheduledmessage.TargetGroup {
		return r.loadActiveChannel(ctx, schedule)
	}
	identity, name, err := r.loadActiveRecipient(ctx, schedule)
	return nil, identity, name, err
}

func (r *Repository) loadActiveChannel(ctx context.Context, schedule *domainscheduledmessage.Schedule) (*domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity, string, error) {
	if schedule.SpaceChannelID == nil {
		return nil, nil, "", interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	var channel domainscheduledmessage.SpaceChannel
	err := r.DB.WithContext(ctx).Where("id = ? AND space_id = ? AND status = ? AND channel = ? AND external_chat_id ~ ?",
		*schedule.SpaceChannelID, schedule.SpaceID, domainscheduledmessage.SpaceChannelStatusActive, whatsappChannel, groupChatIdentifier).First(&channel).Error
	if err != nil {
		return nil, nil, "", err
	}
	return &channel, nil, "WhatsApp group", nil
}

func (r *Repository) loadActiveRecipient(ctx context.Context, schedule *domainscheduledmessage.Schedule) (*domainidentity.ExternalIdentity, string, error) {
	if schedule.RecipientMemberID == nil {
		return nil, "", interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	var identity domainidentity.ExternalIdentity
	var name string
	err := r.DB.WithContext(ctx).Table("external_identities").
		Select("external_identities.*").
		Joins("JOIN space_members ON space_members.user_id = external_identities.user_id").
		Joins("JOIN users ON users.id = external_identities.user_id").
		Where("space_members.id = ? AND space_members.space_id = ? AND space_members.status = ? AND space_members.deleted_at IS NULL AND external_identities.provider = ? AND external_identities.status = ? AND external_identities.deleted_at IS NULL AND external_identities.verified_at IS NOT NULL AND external_identities.external_id ~ ?",
			*schedule.RecipientMemberID, schedule.SpaceID, memberActiveStatus, domainidentity.ProviderHermes, domainidentity.StatusActive, whatsappIdentifier).
		First(&identity).Error
	if err != nil {
		return nil, "", err
	}
	if !isValidWhatsAppIdentity(identity.ExternalID) {
		return nil, "", interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	err = r.DB.WithContext(ctx).Table("users").Select("users.name").
		Joins("JOIN space_members ON space_members.user_id = users.id").
		Where("space_members.id = ? AND space_members.space_id = ? AND space_members.status = ? AND space_members.deleted_at IS NULL", *schedule.RecipientMemberID, schedule.SpaceID, memberActiveStatus).
		Scan(&name).Error
	if err != nil {
		return nil, "", err
	}
	return &identity, strings.TrimSpace(name), nil
}

func isValidWhatsAppIdentity(externalID string) bool {
	return whatsappIdentityPattern.MatchString(strings.TrimSpace(externalID))
}

func (r *Repository) PersistGeneratedContent(ctx context.Context, delivery *domainscheduledmessage.Delivery, content string, updatedAt time.Time) error {
	if delivery == nil || delivery.LeaseToken == nil || delivery.LeaseExpiresAt == nil {
		return interfacescheduledmessage.ErrClaimConflict
	}
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockScheduleByID(tx, delivery.ScheduledMessageID); err != nil {
			return err
		}
		if err := lockDelivery(tx, delivery, updatedAt); err != nil {
			return err
		}
		result := tx.Model(&domainscheduledmessage.Delivery{}).
			Where("id = ? AND status = ? AND lease_token = ? AND lease_until = ? AND lease_until > ?", delivery.ID, domainscheduledmessage.DeliveryStatusPending, *delivery.LeaseToken, *delivery.LeaseExpiresAt, updatedAt).
			Updates(map[string]any{"content_snapshot": content, "updated_at": updatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return interfacescheduledmessage.ErrClaimConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	delivery.ContentSnapshot = &content
	delivery.UpdatedAt = updatedAt
	return nil
}

func (r *Repository) ReserveGenerationAttempt(ctx context.Context, delivery *domainscheduledmessage.Delivery, at time.Time, maxAttempts int) error {
	return r.reserveAttempt(ctx, delivery, at, maxAttempts, "generation_attempts")
}

func (r *Repository) ReserveDeliveryAttempt(ctx context.Context, delivery *domainscheduledmessage.Delivery, at time.Time, maxAttempts int) error {
	return r.reserveAttempt(ctx, delivery, at, maxAttempts, "delivery_attempts")
}

func (r *Repository) reserveAttempt(ctx context.Context, delivery *domainscheduledmessage.Delivery, at time.Time, maxAttempts int, counter string) error {
	if !hasLease(delivery) {
		return interfacescheduledmessage.ErrClaimConflict
	}
	if maxAttempts <= 0 {
		return interfacescheduledmessage.ErrAttemptLimitReached
	}
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockScheduleByID(tx, delivery.ScheduledMessageID); err != nil {
			return err
		}
		if err := lockDelivery(tx, delivery, at); err != nil {
			return err
		}
		result := tx.Model(&domainscheduledmessage.Delivery{}).
			Where("id = ? AND status = ? AND lease_token = ? AND lease_until = ? AND lease_until > ? AND "+counter+" < ?",
				delivery.ID, domainscheduledmessage.DeliveryStatusPending, *delivery.LeaseToken, *delivery.LeaseExpiresAt, at, maxAttempts).
			Updates(map[string]any{counter: gorm.Expr(counter+" + ?", 1), "updated_at": at})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return interfacescheduledmessage.ErrAttemptLimitReached
		}
		return nil
	})
	if err != nil {
		return err
	}
	if counter == "generation_attempts" {
		delivery.GenerationAttempts++
	} else {
		delivery.DeliveryAttempts++
	}
	delivery.UpdatedAt = at
	return nil
}

func (r *Repository) RecordRetry(ctx context.Context, delivery *domainscheduledmessage.Delivery, schedule *domainscheduledmessage.Schedule) error {
	if !hasLease(delivery) || schedule == nil {
		return interfacescheduledmessage.ErrClaimConflict
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := lockScheduleByID(tx, delivery.ScheduledMessageID)
		if err != nil {
			return err
		}
		if err := lockDelivery(tx, delivery, delivery.UpdatedAt); err != nil {
			return err
		}
		if err := updateRetryDelivery(tx, delivery); err != nil {
			return err
		}
		if schedule.Status != locked.Status || schedule.NextScheduledAt != nil {
			return updateSchedule(tx, locked.ID, map[string]any{"status": schedule.Status, "next_scheduled_at": schedule.NextScheduledAt, "updated_at": schedule.UpdatedAt})
		}
		return nil
	})
}

func hasLease(delivery *domainscheduledmessage.Delivery) bool {
	return delivery != nil && delivery.LeaseToken != nil && delivery.LeaseExpiresAt != nil
}

func lockScheduleByID(tx *gorm.DB, scheduleID string) (*domainscheduledmessage.Schedule, error) {
	var schedule domainscheduledmessage.Schedule
	err := tx.Model(&domainscheduledmessage.Schedule{}).
		Joins("JOIN spaces ON spaces.id = scheduled_messages.space_id").
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "scheduled_messages"}}).
		Where("scheduled_messages.id = ? AND scheduled_messages.status = ? AND spaces.status = ? AND spaces.deleted_at IS NULL", scheduleID, domainscheduledmessage.ScheduleStatusActive, spaceActiveStatus).
		First(&schedule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfacescheduledmessage.ErrClaimConflict
	}
	return &schedule, err
}

func lockScheduleForCancellation(tx *gorm.DB, delivery *domainscheduledmessage.Delivery) (*domainscheduledmessage.Schedule, error) {
	spaceID, err := findSpaceIDForDelivery(tx, delivery.ID, *delivery.LeaseToken, delivery.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := lockSpaceByID(tx, spaceID); err != nil {
		return nil, err
	}
	var schedule domainscheduledmessage.Schedule
	err = tx.Model(&domainscheduledmessage.Schedule{}).
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "scheduled_messages"}}).
		Where("id = ? AND space_id = ? AND status = ?", delivery.ScheduledMessageID, spaceID, domainscheduledmessage.ScheduleStatusActive).
		First(&schedule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfacescheduledmessage.ErrClaimConflict
	}
	return &schedule, err
}

func lockDelivery(tx *gorm.DB, claimed *domainscheduledmessage.Delivery, now time.Time) error {
	var delivery domainscheduledmessage.Delivery
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND scheduled_message_id = ? AND status = ? AND lease_token = ? AND lease_until = ? AND lease_until > ?",
			claimed.ID, claimed.ScheduledMessageID, domainscheduledmessage.DeliveryStatusPending, *claimed.LeaseToken, *claimed.LeaseExpiresAt, now).
		First(&delivery).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return interfacescheduledmessage.ErrClaimConflict
	}
	return err
}

func updateRetryDelivery(tx *gorm.DB, delivery *domainscheduledmessage.Delivery) error {
	updates := map[string]any{
		"content_snapshot": delivery.ContentSnapshot, "generation_attempts": delivery.GenerationAttempts,
		"delivery_attempts": delivery.DeliveryAttempts, "next_attempt_at": delivery.NextAttemptAt,
		"lease_token": nil, "lease_until": nil, "status": delivery.Status,
		"failure_category": delivery.FailureCategory, "updated_at": delivery.UpdatedAt,
	}
	result := tx.Model(&domainscheduledmessage.Delivery{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_until = ? AND lease_until > ?", delivery.ID, domainscheduledmessage.DeliveryStatusPending, *delivery.LeaseToken, *delivery.LeaseExpiresAt, delivery.UpdatedAt).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return interfacescheduledmessage.ErrClaimConflict
	}
	return nil
}

func (r *Repository) FinalizeOccurrence(ctx context.Context, delivery *domainscheduledmessage.Delivery, schedule *domainscheduledmessage.Schedule, next *domainscheduledmessage.Delivery) error {
	if !hasLease(delivery) || schedule == nil {
		return interfacescheduledmessage.ErrClaimConflict
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked *domainscheduledmessage.Schedule
		var err error
		if delivery.Status == domainscheduledmessage.DeliveryStatusCancelled {
			locked, err = lockScheduleForCancellation(tx, delivery)
		} else {
			locked, err = lockScheduleByID(tx, delivery.ScheduledMessageID)
		}
		if err != nil {
			return err
		}
		if err := lockDelivery(tx, delivery, delivery.UpdatedAt); err != nil {
			return err
		}
		if err := updateFinalDelivery(tx, delivery); err != nil {
			return err
		}
		if next != nil {
			if err := insertNextOccurrence(tx, locked.ID, next, schedule.UpdatedAt); err != nil {
				return err
			}
		}
		updates := map[string]any{"status": schedule.Status, "next_scheduled_at": schedule.NextScheduledAt, "updated_at": schedule.UpdatedAt}
		return updateSchedule(tx, locked.ID, updates)
	})
}

func updateFinalDelivery(tx *gorm.DB, delivery *domainscheduledmessage.Delivery) error {
	result := tx.Model(&domainscheduledmessage.Delivery{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_until = ? AND lease_until > ?", delivery.ID, domainscheduledmessage.DeliveryStatusPending, *delivery.LeaseToken, *delivery.LeaseExpiresAt, delivery.UpdatedAt).
		Updates(map[string]any{
			"status": delivery.Status, "failure_category": delivery.FailureCategory, "sent_at": delivery.SentAt,
			"next_attempt_at": delivery.NextAttemptAt, "lease_token": nil, "lease_until": nil, "updated_at": delivery.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return interfacescheduledmessage.ErrClaimConflict
	}
	return nil
}

func insertNextOccurrence(tx *gorm.DB, scheduleID string, next *domainscheduledmessage.Delivery, at time.Time) error {
	if strings.TrimSpace(next.ID) == "" {
		next.ID = utils.CreateUUID()
	}
	next.ScheduledMessageID = scheduleID
	next.Status = domainscheduledmessage.DeliveryStatusPending
	next.NextAttemptAt = &next.ScheduledAt
	setCreatedAt(&next.CreatedAt, &next.UpdatedAt, at)
	return tx.Create(next).Error
}

func (r *Repository) DispatchAndFinalize(ctx context.Context, deliveryID, token string, now time.Time, nextOccurrence interfacescheduledmessage.NextOccurrenceFactory, dispatch interfacescheduledmessage.DispatchCallback) error {
	accepted := false
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceID, err := findSpaceIDForDelivery(tx, deliveryID, token, now)
		if err != nil {
			return err
		}
		if err := lockActiveSpace(tx, spaceID); err != nil {
			return err
		}
		schedule, err := lockScheduleForDelivery(tx, deliveryID, token, now, spaceID)
		if err != nil {
			return err
		}
		delivery, err := lockDeliveryByID(tx, deliveryID, schedule.ID, token, now)
		if err != nil {
			return err
		}
		channel, identity, err := lockDispatchTarget(tx, schedule)
		if err != nil {
			return err
		}
		if dispatch == nil {
			return errors.New("scheduled message dispatch callback is required")
		}
		finalizedAt, err := dispatch(ctx, schedule, delivery, channel, identity)
		if err != nil {
			return err
		}
		accepted = true
		if finalizedAt.IsZero() {
			return errors.New("scheduled message dispatch finalization time is required")
		}
		var next *domainscheduledmessage.Delivery
		if nextOccurrence != nil {
			next, err = nextOccurrence(schedule, finalizedAt)
			if err != nil {
				return err
			}
		}
		if err := finalizeDispatchTx(tx, delivery, schedule, next, finalizedAt); err != nil {
			return err
		}
		return nil
	})
	if accepted && err != nil {
		return fmt.Errorf("%w: %w", interfacescheduledmessage.ErrDispatchOutcomeUncertain, err)
	}
	return err
}

func findSpaceIDForDelivery(tx *gorm.DB, deliveryID, token string, now time.Time) (string, error) {
	var result struct{ SpaceID string }
	err := tx.Table("scheduled_message_deliveries").
		Select("scheduled_messages.space_id").
		Joins("JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id").
		Where("scheduled_message_deliveries.id = ? AND scheduled_message_deliveries.status = ? AND scheduled_message_deliveries.lease_token = ? AND scheduled_message_deliveries.lease_until > ?",
			deliveryID, domainscheduledmessage.DeliveryStatusPending, token, now).
		Take(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", interfacescheduledmessage.ErrClaimConflict
	}
	return result.SpaceID, err
}

func lockActiveSpace(tx *gorm.DB, spaceID string) error {
	status, err := lockSpaceByID(tx, spaceID)
	if err == nil && status != spaceActiveStatus {
		return interfacescheduledmessage.ErrInvalidTarget
	}
	return err
}

func lockSpaceByID(tx *gorm.DB, spaceID string) (string, error) {
	var space struct {
		ID     string
		Status string
	}
	err := tx.Table("spaces").Select("id, status").Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND deleted_at IS NULL", spaceID).Take(&space).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", interfacescheduledmessage.ErrInvalidTarget
	}
	return space.Status, err
}

func lockScheduleForDelivery(tx *gorm.DB, deliveryID, token string, now time.Time, spaceID string) (*domainscheduledmessage.Schedule, error) {
	var schedule domainscheduledmessage.Schedule
	err := tx.Model(&domainscheduledmessage.Schedule{}).
		Joins("JOIN scheduled_message_deliveries ON scheduled_message_deliveries.scheduled_message_id = scheduled_messages.id").
		Joins("JOIN spaces ON spaces.id = scheduled_messages.space_id").
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "scheduled_messages"}}).
		Where("scheduled_message_deliveries.id = ? AND scheduled_message_deliveries.status = ? AND scheduled_message_deliveries.lease_token = ? AND scheduled_message_deliveries.lease_until > ? AND scheduled_messages.status = ? AND spaces.status = ? AND spaces.deleted_at IS NULL AND scheduled_messages.space_id = ?",
			deliveryID, domainscheduledmessage.DeliveryStatusPending, token, now, domainscheduledmessage.ScheduleStatusActive, spaceActiveStatus, spaceID).
		First(&schedule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfacescheduledmessage.ErrClaimConflict
	}
	return &schedule, err
}

func lockDeliveryByID(tx *gorm.DB, deliveryID, scheduleID, token string, now time.Time) (*domainscheduledmessage.Delivery, error) {
	var delivery domainscheduledmessage.Delivery
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND scheduled_message_id = ? AND status = ? AND lease_token = ? AND lease_until > ?", deliveryID, scheduleID, domainscheduledmessage.DeliveryStatusPending, token, now).
		First(&delivery).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfacescheduledmessage.ErrClaimConflict
	}
	return &delivery, err
}

func lockDispatchTarget(tx *gorm.DB, schedule *domainscheduledmessage.Schedule) (*domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity, error) {
	roleID, err := lockActiveMember(tx, schedule.SpaceID, schedule.CreatedByMemberID)
	if err != nil {
		return nil, nil, err
	}
	if err := lockCreatorCreatePermission(tx, roleID); err != nil {
		return nil, nil, err
	}
	if schedule.Target == domainscheduledmessage.TargetGroup {
		return lockGroupTarget(tx, schedule)
	}
	identity, err := lockDMTarget(tx, schedule)
	return nil, identity, err
}

func lockGroupTarget(tx *gorm.DB, schedule *domainscheduledmessage.Schedule) (*domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity, error) {
	if schedule.SpaceChannelID == nil {
		return nil, nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	var channel domainscheduledmessage.SpaceChannel
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND space_id = ? AND status = ? AND channel = ? AND external_chat_id ~ ?",
			*schedule.SpaceChannelID, schedule.SpaceID, domainscheduledmessage.SpaceChannelStatusActive, whatsappChannel, groupChatIdentifier).
		First(&channel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	return &channel, nil, err
}

func lockActiveMember(tx *gorm.DB, spaceID, memberID string) (string, error) {
	var member domainspace.Member
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND space_id = ? AND status = ? AND deleted_at IS NULL", memberID, spaceID, memberActiveStatus).
		First(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", interfacescheduledmessage.ErrInvalidTarget
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(member.RoleID) == "" {
		return "", interfacescheduledmessage.ErrInvalidTarget
	}
	return member.RoleID, nil
}

func lockCreatorCreatePermission(tx *gorm.DB, roleID string) error {
	var permissions []struct {
		ID string
	}
	err := tx.Raw(`SELECT p.id FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
JOIN roles r ON r.id = rp.role_id
WHERE rp.role_id = ? AND p.resource = ? AND p.action = ?
  AND r.deleted_at IS NULL AND p.deleted_at IS NULL
FOR SHARE OF r, rp, p`, roleID, scheduledMessagesResource, scheduledMessagesCreate).Scan(&permissions).Error
	if err != nil {
		return err
	}
	if len(permissions) == 0 {
		return interfacescheduledmessage.ErrInvalidTarget
	}
	return nil
}

func lockDMTarget(tx *gorm.DB, schedule *domainscheduledmessage.Schedule) (*domainidentity.ExternalIdentity, error) {
	if schedule.RecipientMemberID == nil {
		return nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	var identity domainidentity.ExternalIdentity
	err := tx.Table("external_identities").Select("external_identities.*").
		Joins("JOIN space_members ON space_members.user_id = external_identities.user_id").
		Joins("JOIN spaces ON spaces.id = space_members.space_id").
		Clauses(clause.Locking{Strength: "SHARE"}).
		Where("space_members.id = ? AND space_members.space_id = ? AND space_members.status = ? AND space_members.deleted_at IS NULL AND spaces.status = ? AND spaces.deleted_at IS NULL AND external_identities.provider = ? AND external_identities.status = ? AND external_identities.deleted_at IS NULL AND external_identities.verified_at IS NOT NULL AND external_identities.external_id ~ ?",
			*schedule.RecipientMemberID, schedule.SpaceID, memberActiveStatus, spaceActiveStatus, domainidentity.ProviderHermes, domainidentity.StatusActive,
			whatsappIdentifier).First(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	if err == nil && !isValidWhatsAppIdentity(identity.ExternalID) {
		return nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	return &identity, err
}

func finalizeDispatchTx(tx *gorm.DB, delivery *domainscheduledmessage.Delivery, schedule *domainscheduledmessage.Schedule, next *domainscheduledmessage.Delivery, now time.Time) error {
	result := tx.Model(&domainscheduledmessage.Delivery{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_until > ?", delivery.ID, domainscheduledmessage.DeliveryStatusPending, *delivery.LeaseToken, now).
		Updates(map[string]any{"status": domainscheduledmessage.DeliveryStatusSent, "sent_at": now, "lease_token": nil, "lease_until": nil, "next_attempt_at": nil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return interfacescheduledmessage.ErrClaimConflict
	}
	status := schedule.Status
	var nextAt *time.Time
	if next != nil {
		if err := insertNextOccurrence(tx, schedule.ID, next, now); err != nil {
			return err
		}
		nextAt = &next.ScheduledAt
	} else {
		status = domainscheduledmessage.ScheduleStatusSent
	}
	updated := tx.Model(&domainscheduledmessage.Schedule{}).Where("id = ? AND status = ?", schedule.ID, domainscheduledmessage.ScheduleStatusActive).
		Updates(map[string]any{"status": status, "next_scheduled_at": nextAt, "updated_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return interfacescheduledmessage.ErrClaimConflict
	}
	return nil
}
