package repositoryreminder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	"family-assistant/utils"

	"github.com/jackc/pgx/v5/pgconn"
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
	return insertReminder(r.DB.WithContext(ctx), reminder)
}

func insertReminder(db *gorm.DB, reminder *domainreminder.Reminder) error {
	return db.
		Select("id", "space_id", "created_by_member_id", "assignee_member_id", "title", "description", "scheduled_at", "status", "delivery_provider", "delivery_target", "completed_at", "created_at", "updated_at", "replace_key").
		Create(reminder).Error
}

const replaceKeyIndexName = "ux_reminders_pending_replace_key"

func isReplaceKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == replaceKeyIndexName
}

// CreateReplacing inserts the reminder and, when it carries a replace key,
// cancels earlier pending reminders and completes earlier sent reminders with
// the same key in the same space within one transaction. A later pending
// reminder is kept and nothing is written.
func (r *Repository) CreateReplacing(ctx context.Context, reminder *domainreminder.Reminder, now time.Time) (domainreminder.ReplaceResult, error) {
	if reminder == nil {
		return domainreminder.ReplaceResult{}, domainreminder.ErrReminderRequired
	}
	if err := validateSpaceID(reminder.SpaceID); err != nil {
		return domainreminder.ReplaceResult{}, err
	}
	if reminder.ReplaceKey == nil {
		if err := r.Create(ctx, reminder); err != nil {
			return domainreminder.ReplaceResult{}, err
		}
		return domainreminder.ReplaceResult{Outcome: domainreminder.ReplaceOutcomeNone, Reminder: reminder}, nil
	}
	if strings.TrimSpace(reminder.ID) == "" {
		reminder.ID = utils.CreateUUID()
	}

	var result domainreminder.ReplaceResult
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		result, err = r.replaceOnce(ctx, reminder, now)
		if err == nil || !isReplaceKeyViolation(err) {
			return result, err
		}
	}
	return domainreminder.ReplaceResult{}, fmt.Errorf("%w: %w", domainreminder.ErrReplaceKeyConflict, err)
}

func (r *Repository) replaceOnce(ctx context.Context, reminder *domainreminder.Reminder, now time.Time) (domainreminder.ReplaceResult, error) {
	var result domainreminder.ReplaceResult
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var matching []domainreminder.Reminder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("space_id = ? AND replace_key = ? AND status IN ?", reminder.SpaceID, *reminder.ReplaceKey,
				[]domainreminder.Status{domainreminder.StatusPending, domainreminder.StatusSent}).
			Order("scheduled_at DESC").
			Find(&matching).Error; err != nil {
			return err
		}
		var pending, sent []domainreminder.Reminder
		for i := range matching {
			switch {
			case matching[i].Status == domainreminder.StatusPending:
				pending = append(pending, matching[i])
			case !matching[i].ScheduledAt.After(reminder.ScheduledAt):
				sent = append(sent, matching[i])
			}
		}
		if len(pending) > 0 && pending[0].ScheduledAt.After(reminder.ScheduledAt) {
			kept := pending[0]
			result = domainreminder.ReplaceResult{Outcome: domainreminder.ReplaceOutcomeKeptExisting, Reminder: &kept}
			return nil
		}
		if len(pending) > 0 {
			if err := tx.Model(&domainreminder.Reminder{}).
				Where("id IN ?", reminderIDs(pending)).
				Updates(map[string]any{"status": domainreminder.StatusCancelled, "deleted_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		// A sent reminder was due before the new event, so the new event handled it.
		if len(sent) > 0 {
			if err := tx.Model(&domainreminder.Reminder{}).
				Where("id IN ?", reminderIDs(sent)).
				Updates(map[string]any{"status": domainreminder.StatusCompleted, "completed_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if err := insertReminder(tx, reminder); err != nil {
			return err
		}
		result = domainreminder.ReplaceResult{Outcome: domainreminder.ReplaceOutcomeCreated, Reminder: reminder, Completed: sent}
		if len(pending) > 0 {
			result.Outcome = domainreminder.ReplaceOutcomeReplaced
			result.Replaced = pending
		}
		return nil
	})
	if err != nil {
		return domainreminder.ReplaceResult{}, err
	}
	return result, nil
}

func reminderIDs(reminders []domainreminder.Reminder) []string {
	ids := make([]string, 0, len(reminders))
	for i := range reminders {
		ids = append(ids, reminders[i].ID)
	}
	return ids
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

func (r *Repository) ListPendingForNotification(ctx context.Context) ([]domainreminder.Reminder, error) {
	var reminders []domainreminder.Reminder
	err := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Select("id", "scheduled_at").
		Where("status = ? AND notified_at IS NULL AND deleted_at IS NULL", domainreminder.StatusPending).
		Order("scheduled_at ASC").
		Find(&reminders).Error
	return reminders, err
}

func (r *Repository) MarkNotificationSent(ctx context.Context, reminderID string, claimedAt, sentAt time.Time) error {
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}
	if claimedAt.IsZero() {
		return domainreminder.ErrStatusConflict
	}
	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND status = ? AND notified_at IS NULL AND notification_claimed_at = ?", reminderID, domainreminder.StatusPending, claimedAt).
		Updates(map[string]any{"notified_at": sentAt, "notification_claimed_at": nil, "status": domainreminder.StatusSent, "updated_at": sentAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainreminder.ErrStatusConflict
	}
	return nil
}

func (r *Repository) ReleaseNotificationClaim(ctx context.Context, reminderID string, claimedAt time.Time) error {
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}
	if claimedAt.IsZero() {
		return domainreminder.ErrStatusConflict
	}
	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND status = ? AND notified_at IS NULL AND notification_claimed_at = ?", reminderID, domainreminder.StatusPending, claimedAt).
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

func (r *Repository) CompleteUnfinished(ctx context.Context, spaceID, reminderID string, completedAt time.Time) error {
	if err := validateSpaceID(spaceID); err != nil {
		return err
	}
	if strings.TrimSpace(reminderID) == "" {
		return domainreminder.ErrReminderIDRequired
	}

	result := r.DB.WithContext(ctx).
		Model(&domainreminder.Reminder{}).
		Where("id = ? AND space_id = ? AND status IN ?", reminderID, spaceID, []domainreminder.Status{domainreminder.StatusPending, domainreminder.StatusSent}).
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
