package repositoryfinance

import (
	"context"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	repositorygeneric "family-assistant/internal/repositories/generic"
	"family-assistant/utils"
)

func (r *Repository) CreateTransaction(ctx context.Context, tx *domainfinance.Transaction) error {
	if tx == nil {
		return ErrIDRequired
	}
	if err := requireSpace(tx.SpaceID); err != nil {
		return err
	}
	if strings.TrimSpace(tx.ID) == "" {
		tx.ID = utils.CreateUUID()
	}
	return r.db(ctx).Create(tx).Error
}

func (r *Repository) FindTransaction(ctx context.Context, spaceID, id string) (*domainfinance.Transaction, error) {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return nil, err
	}
	var tx domainfinance.Transaction
	if err := r.db(ctx).Where("space_id = ? AND id = ?", spaceID, id).First(&tx).Error; err != nil {
		return nil, err
	}
	return &tx, nil
}

// ListTransactions returns the newest transactions first. A transfer matches
// an AccountID filter on either side. From is inclusive and To exclusive.
func (r *Repository) ListTransactions(ctx context.Context, filter domainfinance.ListFilter) ([]domainfinance.Transaction, error) {
	if err := requireSpace(filter.SpaceID); err != nil {
		return nil, err
	}
	query := r.db(ctx).Where("space_id = ?", filter.SpaceID)
	if v := strings.TrimSpace(filter.Type); v != "" {
		query = query.Where("type = ?", v)
	}
	if v := strings.TrimSpace(filter.CategoryID); v != "" {
		query = query.Where("category_id = ?", v)
	}
	if v := strings.TrimSpace(filter.AccountID); v != "" {
		query = query.Where("(account_id = ? OR to_account_id = ?)", v, v)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		search = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		query = repositorygeneric.BuildSearchFunc("merchant", "note")(query, search)
	}
	if filter.From != nil {
		query = query.Where("occurred_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("occurred_at < ?", *filter.To)
	}
	query = query.Order("occurred_at DESC, id DESC")
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	var txs []domainfinance.Transaction
	if err := query.Find(&txs).Error; err != nil {
		return nil, err
	}
	return txs, nil
}

// UpdateTransaction writes every editable column. Select makes GORM write nil
// pointers as NULL (a plain struct update would skip them and never clear a
// category or transfer target).
func (r *Repository) UpdateTransaction(ctx context.Context, tx *domainfinance.Transaction) error {
	if tx == nil {
		return ErrIDRequired
	}
	if err := requireSpaceAndID(tx.SpaceID, tx.ID); err != nil {
		return err
	}
	tx.UpdatedAt = time.Now().UTC()
	result := r.db(ctx).Model(&domainfinance.Transaction{}).
		Where("id = ? AND space_id = ?", tx.ID, tx.SpaceID).
		Select("type", "amount", "account_id", "to_account_id", "category_id", "merchant", "note", "occurred_at", "updated_at").
		Updates(tx)
	return checkAffected(result)
}

func (r *Repository) SoftDeleteTransaction(ctx context.Context, spaceID, id string, at time.Time) error {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Transaction{}, spaceID, id, stamp("deleted_at", at))
}

func (r *Repository) FindRecentSimilar(ctx context.Context, spaceID string, t domainfinance.Transaction, window time.Duration) (*domainfinance.Transaction, error) {
	if err := requireSpace(spaceID); err != nil {
		return nil, err
	}
	ref := t.CreatedAt
	if ref.IsZero() {
		ref = time.Now().UTC()
	}
	query := r.db(ctx).Where(
		"space_id = ? AND type = ? AND amount = ? AND account_id = ? AND to_account_id IS NOT DISTINCT FROM ? AND category_id IS NOT DISTINCT FROM ? AND COALESCE(merchant, '') = ? AND created_at >= ? AND created_at <= ?",
		spaceID, t.Type, t.Amount, t.AccountID, t.ToAccountID, t.CategoryID, t.Merchant, ref.Add(-window), ref)
	if t.ID != "" {
		query = query.Where("id <> ?", t.ID)
	}
	var found []domainfinance.Transaction
	if err := query.Order("created_at DESC").Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}
