package interfacefinance

import (
	"context"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
)

// RepoFinanceInterface is the persistence port for finance. Every method is
// scoped by Space id and ignores soft-deleted rows. Find* and the
// Update/Archive/Delete methods return gorm.ErrRecordNotFound when no live row
// of that Space matches, so a foreign Space id looks like a missing row.
type RepoFinanceInterface interface {
	CreateAccount(ctx context.Context, account *domainfinance.Account) error
	ListAccounts(ctx context.Context, spaceID string, includeArchived bool) ([]domainfinance.Account, error)
	FindAccount(ctx context.Context, spaceID, id string) (*domainfinance.Account, error)
	// UpdateAccount writes name, type and opening balance of account.ID.
	UpdateAccount(ctx context.Context, account *domainfinance.Account) error
	ArchiveAccount(ctx context.Context, spaceID, id string, at time.Time) error
	// UnarchiveAccount clears archived_at (writes NULL).
	UnarchiveAccount(ctx context.Context, spaceID, id string) error

	CreateCategory(ctx context.Context, category *domainfinance.Category) error
	ListCategories(ctx context.Context, spaceID string, includeArchived bool) ([]domainfinance.Category, error)
	FindCategory(ctx context.Context, spaceID, id string) (*domainfinance.Category, error)
	// UpdateCategory writes name and kind of category.ID.
	UpdateCategory(ctx context.Context, category *domainfinance.Category) error
	ArchiveCategory(ctx context.Context, spaceID, id string, at time.Time) error
	// UnarchiveCategory clears archived_at (writes NULL).
	UnarchiveCategory(ctx context.Context, spaceID, id string) error
	// SeedDefaultCategories inserts defaults in one transaction guarded by a
	// per-Space advisory lock, and only when the Space has no category row of
	// any state (archived and soft-deleted included). It is idempotent and safe
	// across app instances; each default gets SpaceID and, when empty, an id.
	SeedDefaultCategories(ctx context.Context, spaceID string, defaults []domainfinance.Category) error

	CreateTransaction(ctx context.Context, tx *domainfinance.Transaction) error
	FindTransaction(ctx context.Context, spaceID, id string) (*domainfinance.Transaction, error)
	ListTransactions(ctx context.Context, filter domainfinance.ListFilter) ([]domainfinance.Transaction, error)
	// UpdateTransaction overwrites every editable column, so nil ToAccountID or
	// CategoryID clears the column to NULL.
	UpdateTransaction(ctx context.Context, tx *domainfinance.Transaction) error
	SoftDeleteTransaction(ctx context.Context, spaceID, id string, at time.Time) error
	// FindRecentSimilar returns a live transaction with the same type, amount,
	// accounts, category and merchant created within window before t.CreatedAt
	// (now when zero), or nil, nil when there is none.
	FindRecentSimilar(ctx context.Context, spaceID string, t domainfinance.Transaction, window time.Duration) (*domainfinance.Transaction, error)

	// UpsertBudget sets the budget of (space, category, month); the month is
	// normalized with domainfinance.MonthDate.
	UpsertBudget(ctx context.Context, budget *domainfinance.Budget) error
	ListBudgets(ctx context.Context, spaceID string, month time.Time) ([]domainfinance.Budget, error)
	DeleteBudget(ctx context.Context, spaceID, id string, at time.Time) error

	// Balances returns every non-deleted account of the Space, archived ones
	// included, with its computed balance.
	Balances(ctx context.Context, spaceID string) ([]domainfinance.AccountBalance, error)
	// Summary and SpentByCategory take already-resolved half-open UTC
	// instants [from, to).
	Summary(ctx context.Context, spaceID string, from, to time.Time) (*domainfinance.SummaryResult, error)
	SpentByCategory(ctx context.Context, spaceID, categoryID string, from, to time.Time) (domainfinance.Money, error)

	CreateRecurring(ctx context.Context, r *domainfinance.Recurring) error
	FindRecurring(ctx context.Context, spaceID, id string) (*domainfinance.Recurring, error)
	ListRecurring(ctx context.Context, spaceID string) ([]domainfinance.Recurring, error)
	// UpdateRecurring writes every editable column of r only while the row's
	// updated_at equals expectedUpdatedAt (optimistic concurrency against the
	// scheduler); a stale or missing row is gorm.ErrRecordNotFound.
	UpdateRecurring(ctx context.Context, r *domainfinance.Recurring, expectedUpdatedAt time.Time) error
	DeleteRecurring(ctx context.Context, spaceID, id string, at time.Time) error
	// PostDueRecurring posts the due occurrences of up to limit templates (at
	// most maxPerTemplate each, the rest fast-forwarded and counted as
	// dropped) and advances their next_run_at, all in one transaction that
	// locks the templates with FOR UPDATE SKIP LOCKED, so an occurrence is
	// never posted twice nor lost on a crash (posted rows carry recurring_id,
	// unique per occurrence). build makes each transaction; a failing
	// template is paused (with pause_reason) or, for an unclassified error,
	// skipped with failure_count and a sanitized last_failure_code until
	// MaxConsecutiveFailures pauses it, all under a savepoint without
	// affecting the others. See the repository for the full contract.
	PostDueRecurring(ctx context.Context, now time.Time, limit, maxPerTemplate int, build domainfinance.RecurringBuildFunc) (domainfinance.PostResult, error)
}
