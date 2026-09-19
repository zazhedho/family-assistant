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
	if strings.TrimSpace(reminder.ID) == "" {
		reminder.ID = utils.CreateUUID()
	}
	return r.DB.WithContext(ctx).
		Select("id", "family_id", "owner_member_id", "created_by_member_id", "scope", "title", "description", "scheduled_at", "status", "completed_at", "created_at", "updated_at").
		Create(reminder).Error
}

func (r *Repository) FindByIDInFamily(ctx context.Context, familyID, reminderID string) (*domainreminder.Reminder, error) {
	if err := validateFamilyID(familyID); err != nil {
		return nil, err
	}

	var reminder domainreminder.Reminder
	if err := r.DB.WithContext(ctx).
		Where("family_id = ? AND id = ?", familyID, reminderID).
		First(&reminder).Error; err != nil {
		return nil, err
	}
	return &reminder, nil
}

func (r *Repository) List(ctx context.Context, filter domainreminder.ListFilter) ([]domainreminder.Reminder, error) {
	if err := validateFamilyID(filter.FamilyID); err != nil {
		return nil, err
	}

	query := r.DB.WithContext(ctx).Where("family_id = ?", filter.FamilyID)
	if filter.OwnerMemberID != nil {
		query = query.Where("owner_member_id = ?", *filter.OwnerMemberID)
	}
	if filter.Scope != nil {
		query = query.Where("scope = ?", *filter.Scope)
	}
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

func (r *Repository) Update(ctx context.Context, reminder *domainreminder.Reminder) error {
	if reminder == nil {
		return domainreminder.ErrReminderRequired
	}
	if err := validateFamilyID(reminder.FamilyID); err != nil {
		return err
	}
	if strings.TrimSpace(reminder.ID) == "" {
		return domainreminder.ErrReminderIDRequired
	}
	reminder.UpdatedAt = time.Now().UTC()

	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND family_id = ? AND status = ?", reminder.ID, reminder.FamilyID, domainreminder.StatusPending).
		Updates(map[string]any{
			"status":       reminder.Status,
			"completed_at": reminder.CompletedAt,
			"updated_at":   reminder.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainreminder.ErrStatusConflict
	}
	return nil
}

func validateFamilyID(familyID string) error {
	if strings.TrimSpace(familyID) == "" {
		return domainreminder.ErrFamilyIDRequired
	}
	return nil
}
