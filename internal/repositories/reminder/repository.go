package repositoryreminder

import (
	"context"
	"strings"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	"family-assistant/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfacereminder.RepoReminderInterface {
	return &Repository{DB: db}
}

func NewSchedulerRepository(db *gorm.DB) interfacereminder.SchedulerRepository {
	return &Repository{DB: db}
}

var _ interfacereminder.RepoReminderInterface = (*Repository)(nil)
var _ interfacereminder.SchedulerRepository = (*Repository)(nil)

func (r *Repository) Create(ctx context.Context, reminder *domainreminder.Reminder) error {
	if reminder == nil {
		return domainreminder.ErrReminderRequired
	}
	if err := validateSpaceID(reminder.SpaceID); err != nil {
		return err
	}
	if strings.TrimSpace(reminder.ID) == "" {
		reminder.ID = utils.CreateUUID()
	}
	return r.DB.WithContext(ctx).
		Select("id", "space_id", "created_by_member_id", "assignee_member_id", "title", "description", "scheduled_at", "status", "delivery_provider", "delivery_target", "completed_at", "created_at", "updated_at").
		Create(reminder).Error
}

func (r *Repository) ClaimDueForNotification(ctx context.Context, now, staleBefore time.Time, limit int) ([]domainreminder.Reminder, error) {
	if limit <= 0 {
		limit = 50
	}

	var reminders []domainreminder.Reminder
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domainreminder.Reminder{}).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND scheduled_at <= ? AND notified_at IS NULL AND deleted_at IS NULL AND (notification_claimed_at IS NULL OR notification_claimed_at < ?)", domainreminder.StatusPending, now, staleBefore).
			Order("scheduled_at ASC").
			Limit(limit).
			Find(&reminders).Error; err != nil {
			return err
		}
		if len(reminders) == 0 {
			return nil
		}

		ids := make([]string, 0, len(reminders))
		for i := range reminders {
			ids = append(ids, reminders[i].ID)
		}
		if err := tx.Model(&domainreminder.Reminder{}).
			Where("id IN ? AND status = ? AND notified_at IS NULL", ids, domainreminder.StatusPending).
			Updates(map[string]any{"notification_claimed_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		for i := range reminders {
			claimedAt := now
			reminders[i].NotificationClaimedAt = &claimedAt
		}
		return nil
	})
	return reminders, err
}

func (r *Repository) MarkNotificationSent(ctx context.Context, reminderID string, sentAt time.Time) error {
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}
	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND status = ? AND notified_at IS NULL AND notification_claimed_at IS NOT NULL", reminderID, domainreminder.StatusPending).
		Updates(map[string]any{"notified_at": sentAt, "notification_claimed_at": nil, "updated_at": sentAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainreminder.ErrStatusConflict
	}
	return nil
}

func (r *Repository) ReleaseNotificationClaim(ctx context.Context, reminderID string) error {
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}
	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND status = ? AND notified_at IS NULL", reminderID, domainreminder.StatusPending).
		Updates(map[string]any{"notification_claimed_at": nil, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainreminder.ErrStatusConflict
	}
	return nil
}

func (r *Repository) FindByIDInSpace(ctx context.Context, spaceID, reminderID string) (*domainreminder.Reminder, error) {
	if err := validateSpaceID(spaceID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(reminderID) == "" {
		return nil, domainreminder.ErrReminderIDRequired
	}

	var reminder domainreminder.Reminder
	if err := r.DB.WithContext(ctx).
		Where("space_id = ? AND id = ?", spaceID, reminderID).
		First(&reminder).Error; err != nil {
		return nil, err
	}
	return &reminder, nil
}

func (r *Repository) List(ctx context.Context, filter domainreminder.ListFilter) ([]domainreminder.Reminder, error) {
	if err := validateSpaceID(filter.SpaceID); err != nil {
		return nil, err
	}

	query := r.DB.WithContext(ctx).Where("space_id = ?", filter.SpaceID)
	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	if filter.From != nil {
		query = query.Where("scheduled_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("scheduled_at <= ?", *filter.To)
	}

	var reminders []domainreminder.Reminder
	if err := query.Find(&reminders).Error; err != nil {
		return nil, err
	}
	return reminders, nil
}

func (r *Repository) CompletePending(ctx context.Context, spaceID, reminderID string, completedAt time.Time) error {
	if err := validateSpaceID(spaceID); err != nil {
		return err
	}
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}

	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND space_id = ? AND status = ?", reminderID, spaceID, domainreminder.StatusPending).
		Updates(map[string]any{
			"completed_at": completedAt,
			"status":       domainreminder.StatusCompleted,
			"updated_at":   completedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainreminder.ErrStatusConflict
	}
	return nil
}

func (r *Repository) UpdatePending(ctx context.Context, spaceID, reminderID string, fields domainreminder.UpdateFields, updatedAt time.Time) error {
	if err := validateSpaceID(spaceID); err != nil {
		return err
	}
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}
	updates := map[string]any{"updated_at": updatedAt}
	if fields.Title != nil {
		updates["title"] = strings.TrimSpace(*fields.Title)
	}
	if fields.Description != nil {
		updates["description"] = *fields.Description
	}
	if fields.ScheduledAt != nil {
		updates["scheduled_at"] = *fields.ScheduledAt
	}
	if fields.ClearAssignee {
		updates["assignee_member_id"] = nil
	} else if fields.AssigneeMemberID != nil {
		updates["assignee_member_id"] = strings.TrimSpace(*fields.AssigneeMemberID)
	}
	if len(updates) == 1 {
		return domainreminder.ErrReminderUpdateRequired
	}

	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND space_id = ? AND status = ?", reminderID, spaceID, domainreminder.StatusPending).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainreminder.ErrStatusConflict
	}
	return nil
}

func (r *Repository) SoftDelete(ctx context.Context, spaceID, reminderID string, status domainreminder.Status, deletedAt time.Time) error {
	if err := validateSpaceID(spaceID); err != nil {
		return err
	}
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}
	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND space_id = ?", reminderID, spaceID).
		Updates(map[string]any{"status": status, "deleted_at": deletedAt, "updated_at": deletedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func validateSpaceID(spaceID string) error {
	if strings.TrimSpace(spaceID) == "" {
		return domainreminder.ErrSpaceIDRequired
	}
	return nil
}
