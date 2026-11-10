package repositoryfinance

import (
	"context"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/utils"
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

// UpdateRecurring writes every editable column, but only while the row's
// updated_at still equals expectedUpdatedAt (the value the caller loaded):
// every scheduler advance or pause stamps updated_at, so an edit built from a
// stale copy can never write an old next_run_at back and repost an
// occurrence. A stale or missing row reports gorm.ErrRecordNotFound. Select
// makes nil pointers (category, transfer target, day of month, end) write NULL.
func (r *Repository) UpdateRecurring(ctx context.Context, rec *domainfinance.Recurring, expectedUpdatedAt time.Time) error {
	if rec == nil {
		return ErrIDRequired
	}
	if err := requireSpaceAndID(rec.SpaceID, rec.ID); err != nil {
		return err
	}
	rec.UpdatedAt = time.Now().UTC()
	result := r.db(ctx).Model(&domainfinance.Recurring{}).
		Where("id = ? AND space_id = ? AND updated_at = ?", rec.ID, rec.SpaceID, expectedUpdatedAt).
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
