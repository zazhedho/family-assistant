package dto

import (
	"time"

	domainfinance "family-assistant/internal/domain/finance"
)

type AccountCreateInput struct {
	Space          string
	Name           string
	Type           string
	OpeningBalance int64
}

type AccountListInput struct {
	Space           string
	IncludeArchived bool
	Limit           int
}

// AccountUpdateInput renames, archives (Archived true) or unarchives
// (Archived false) an account; nil leaves the archive state unchanged.
type AccountUpdateInput struct {
	Space    string
	ID       string
	Name     *string
	Archived *bool
}

type CategoryCreateInput struct {
	Space string
	Name  string
	Kind  string
}

type CategoryListInput struct {
	Space           string
	Kind            string
	IncludeArchived bool
	Limit           int
}

// CategoryUpdateInput renames and/or archives a category.
type CategoryUpdateInput struct {
	Space    string
	ID       string
	Name     *string
	Archived *bool
}

// TransactionCreateInput. Account, ToAccount and Category accept a UUID or an
// exact case-insensitive name. A zero OccurredAt means now.
type TransactionCreateInput struct {
	Space      string
	Type       string
	Amount     int64
	Account    string
	ToAccount  string
	Category   string
	Merchant   string
	Note       string
	OccurredAt time.Time
}

// TransactionListInput filters a listing; Category and Account accept a UUID or
// a name. From is inclusive and To exclusive.
type TransactionListInput struct {
	Space    string
	Type     string
	Category string
	Account  string
	Search   string
	From     *time.Time
	To       *time.Time
	Limit    int
}

// TransactionUpdateInput changes only the fields that are set. An empty string
// for ToAccount or Category clears it.
type TransactionUpdateInput struct {
	Type       *string
	Amount     *int64
	Account    *string
	ToAccount  *string
	Category   *string
	Merchant   *string
	Note       *string
	OccurredAt *time.Time
}

// TransactionResult is the outcome of a create or update. PossibleDuplicate is
// a warning only; BudgetStatus is set for an expense whose category has a
// budget in the transaction's month.
type TransactionResult struct {
	Transaction       domainfinance.Transaction
	PossibleDuplicate *domainfinance.Transaction
	BudgetStatus      *domainfinance.BudgetStatus
}
