package repositoryfinance

import (
	"context"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
)

// Balances computes opening_balance + income - expense + transfers in -
// transfers out per account in SQL, ignoring soft-deleted transactions. Every
// non-deleted account is returned, archived ones included.
func (r *Repository) Balances(ctx context.Context, spaceID string) ([]domainfinance.AccountBalance, error) {
	if err := requireSpace(spaceID); err != nil {
		return nil, err
	}
	const query = `SELECT a.id AS account_id, a.name AS name, a.type AS type,
 (a.opening_balance + COALESCE(SUM(CASE
   WHEN t.account_id = a.id AND t.type = 'income' THEN t.amount
   WHEN t.account_id = a.id AND t.type = 'expense' THEN -t.amount
   WHEN t.account_id = a.id AND t.type = 'transfer' THEN -t.amount
   WHEN t.to_account_id = a.id AND t.type = 'transfer' THEN t.amount
   ELSE 0 END), 0))::bigint AS balance
FROM finance_accounts a
LEFT JOIN finance_transactions t ON t.space_id = a.space_id AND t.deleted_at IS NULL AND (t.account_id = a.id OR t.to_account_id = a.id)
WHERE a.space_id = ? AND a.deleted_at IS NULL
GROUP BY a.id, a.name, a.type, a.opening_balance, a.created_at
ORDER BY a.created_at, a.id`
	balances := []domainfinance.AccountBalance{}
	if err := r.db(ctx).Raw(query, spaceID).Scan(&balances).Error; err != nil {
		return nil, err
	}
	return balances, nil
}

// Summary aggregates the Space's transactions in [from, to). Transfers are
// never income or expense. ByCategory and TopMerchants count expenses only;
// ByAccount reports income and expense per account.
func (r *Repository) Summary(ctx context.Context, spaceID string, from, to time.Time) (*domainfinance.SummaryResult, error) {
	if err := requireSpace(spaceID); err != nil {
		return nil, err
	}
	db := r.db(ctx)
	result := &domainfinance.SummaryResult{
		ByCategory: []domainfinance.CategoryTotal{}, ByAccount: []domainfinance.AccountTotal{}, TopMerchants: []domainfinance.MerchantTotal{},
	}

	var totals struct{ Income, Expense int64 }
	if err := db.Raw(`SELECT COALESCE(SUM(amount) FILTER (WHERE type = 'income'), 0)::bigint AS income,
 COALESCE(SUM(amount) FILTER (WHERE type = 'expense'), 0)::bigint AS expense
FROM finance_transactions WHERE space_id = ? AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at < ?`,
		spaceID, from, to).Scan(&totals).Error; err != nil {
		return nil, err
	}
	result.Income, result.Expense = totals.Income, totals.Expense

	if err := db.Raw(`SELECT COALESCE(t.category_id::text, '') AS category_id, COALESCE(c.name, '') AS name, SUM(t.amount)::bigint AS total
FROM finance_transactions t
LEFT JOIN finance_categories c ON c.space_id = t.space_id AND c.id = t.category_id
WHERE t.space_id = ? AND t.deleted_at IS NULL AND t.type = 'expense' AND t.occurred_at >= ? AND t.occurred_at < ?
GROUP BY t.category_id, c.name
ORDER BY total DESC, name`, spaceID, from, to).Scan(&result.ByCategory).Error; err != nil {
		return nil, err
	}

	if err := db.Raw(`SELECT t.account_id::text AS account_id, a.name AS name,
 COALESCE(SUM(t.amount) FILTER (WHERE t.type = 'income'), 0)::bigint AS income,
 COALESCE(SUM(t.amount) FILTER (WHERE t.type = 'expense'), 0)::bigint AS expense
FROM finance_transactions t
JOIN finance_accounts a ON a.space_id = t.space_id AND a.id = t.account_id
WHERE t.type IN ('income', 'expense') AND t.space_id = ? AND t.deleted_at IS NULL AND t.occurred_at >= ? AND t.occurred_at < ?
GROUP BY t.account_id, a.name
ORDER BY a.name, t.account_id`, spaceID, from, to).Scan(&result.ByAccount).Error; err != nil {
		return nil, err
	}

	if err := db.Raw(`SELECT merchant AS merchant, SUM(amount)::bigint AS total, COUNT(*)::int AS count
FROM finance_transactions
WHERE space_id = ? AND deleted_at IS NULL AND type = 'expense' AND COALESCE(merchant, '') <> '' AND occurred_at >= ? AND occurred_at < ?
GROUP BY merchant ORDER BY total DESC, merchant LIMIT 5`, spaceID, from, to).Scan(&result.TopMerchants).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// SpentByCategory sums the Space's live expenses of one category in [from, to).
func (r *Repository) SpentByCategory(ctx context.Context, spaceID, categoryID string, from, to time.Time) (int64, error) {
	if err := requireSpaceAndID(spaceID, categoryID); err != nil {
		return 0, err
	}
	var row struct{ Total int64 }
	if err := r.db(ctx).Raw(`SELECT COALESCE(SUM(amount), 0)::bigint AS total
FROM finance_transactions
WHERE space_id = ? AND deleted_at IS NULL AND type = 'expense' AND category_id = ? AND occurred_at >= ? AND occurred_at < ?`,
		spaceID, categoryID, from, to).Scan(&row).Error; err != nil {
		return 0, err
	}
	return row.Total, nil
}
