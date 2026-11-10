package repositoryfinance

import (
	"context"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/utils"

	"gorm.io/gorm/clause"
)

// UpsertBudget inserts the budget or, when a live budget already exists for
// (space, category, month), overwrites its amount. The conflict target is the
// partial unique index, so a soft-deleted budget does not block a new one. The
// stored row (with the id of the existing budget on conflict) is written back
// into budget.
func (r *Repository) UpsertBudget(ctx context.Context, budget *domainfinance.Budget) error {
	if budget == nil {
		return ErrIDRequired
	}
	if err := requireSpace(budget.SpaceID); err != nil {
		return err
	}
	if strings.TrimSpace(budget.ID) == "" {
		budget.ID = utils.CreateUUID()
	}
	budget.Month = domainfinance.MonthDate(budget.Month)
	return r.db(ctx).Clauses(
		clause.OnConflict{
			Columns:     []clause.Column{{Name: "space_id"}, {Name: "category_id"}, {Name: "month"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
			DoUpdates:   clause.AssignmentColumns([]string{"amount", "updated_at"}),
		},
		clause.Returning{},
	).Create(budget).Error
}

func (r *Repository) ListBudgets(ctx context.Context, spaceID string, month time.Time) ([]domainfinance.Budget, error) {
	if err := requireSpace(spaceID); err != nil {
		return nil, err
	}
	var budgets []domainfinance.Budget
	if err := r.db(ctx).Where("space_id = ? AND month = ?", spaceID, domainfinance.MonthDate(month)).
		Order("created_at, id").Find(&budgets).Error; err != nil {
		return nil, err
	}
	return budgets, nil
}

func (r *Repository) DeleteBudget(ctx context.Context, spaceID, id string, at time.Time) error {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Budget{}, spaceID, id, stamp("deleted_at", at))
}
