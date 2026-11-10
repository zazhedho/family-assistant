package repositoryfinance

import (
	"context"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) CreateRecurring(ctx context.Context, rec *domainfinance.Recurring) error {
	if rec == nil {
		return ErrIDRequired
	}
	if err := requireSpace(rec.SpaceID); err != nil {
		return err
	}
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = utils.CreateUUID()
	}
	return r.db(ctx).Create(rec).Error
}

func (r *Repository) FindRecurring(ctx context.Context, spaceID, id string) (*domainfinance.Recurring, error) {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return nil, err
	}
	var rec domainfinance.Recurring
	if err := r.db(ctx).Where("space_id = ? AND id = ?", spaceID, id).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *Repository) ListRecurring(ctx context.Context, spaceID string) ([]domainfinance.Recurring, error) {
	if err := requireSpace(spaceID); err != nil {
		return nil, err
	}
	var recs []domainfinance.Recurring
	if err := r.db(ctx).Where("space_id = ?", spaceID).Order("next_run_at, id").Find(&recs).Error; err != nil {
		return nil, err
	}
	return recs, nil
}

// UpdateRecurring writes every editable column; Select makes nil pointers
// (category, transfer target, day of month, end) write NULL.
func (r *Repository) UpdateRecurring(ctx context.Context, rec *domainfinance.Recurring) error {
	if rec == nil {
		return ErrIDRequired
	}
	if err := requireSpaceAndID(rec.SpaceID, rec.ID); err != nil {
		return err
	}
	rec.UpdatedAt = time.Now().UTC()
	result := r.db(ctx).Model(&domainfinance.Recurring{}).
		Where("id = ? AND space_id = ?", rec.ID, rec.SpaceID).
		Select("type", "amount", "account_id", "to_account_id", "category_id", "merchant", "note", "frequency", "interval_n",
			"day_of_month", "start_at", "next_run_at", "end_at", "paused", "updated_at").
		Updates(rec)
	return checkAffected(result)
}

func (r *Repository) DeleteRecurring(ctx context.Context, spaceID, id string, at time.Time) error {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Recurring{}, spaceID, id, stamp("deleted_at", at))
}

// ClaimDueRecurring locks up to limit due schedules with FOR UPDATE SKIP
// LOCKED (so overlapping scheduler ticks never claim the same row) and moves
// each next_run_at forward by one occurrence in the same transaction; a failed
// advance rolls everything back. A schedule past its end_at is never due.
//
// The returned rows keep the PRE-advance NextRunAt, which is the occurrence the
// caller must post. When a schedule is several occurrences behind, the next
// call claims the following one, so a catch-up caller loops until it has posted
// its cap (12) and then moves the schedule on itself.
func (r *Repository) ClaimDueRecurring(ctx context.Context, now time.Time, limit int) ([]domainfinance.Recurring, error) {
	if limit <= 0 {
		return nil, nil
	}
	var claimed []domainfinance.Recurring
	err := r.db(ctx).Transaction(func(tx *gorm.DB) error {
		var due []domainfinance.Recurring
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("paused = ? AND next_run_at <= ? AND (end_at IS NULL OR next_run_at <= end_at)", false, now).
			Order("next_run_at, id").Limit(limit).Find(&due).Error; err != nil {
			return err
		}
		for _, rec := range due {
			next := domainfinance.NextOccurrence(rec, rec.NextRunAt)
			if err := updateScoped(tx, &domainfinance.Recurring{}, rec.SpaceID, rec.ID,
				map[string]any{"next_run_at": next, "updated_at": now}); err != nil {
				return err
			}
		}
		claimed = due
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}
