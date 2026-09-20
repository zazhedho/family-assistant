package repositoryreminder

import (
	"context"
	"strings"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	"family-assistant/utils"

	"gorm.io/gorm"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{DB: db}
}

var _ domainreminder.Repository = (*Repository)(nil)

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
		Select("id", "space_id", "created_by_member_id", "assignee_member_id", "title", "description", "scheduled_at", "status", "completed_at", "created_at", "updated_at").
		Create(reminder).Error
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

func validateSpaceID(spaceID string) error {
	if strings.TrimSpace(spaceID) == "" {
		return domainreminder.ErrSpaceIDRequired
	}
	return nil
}
