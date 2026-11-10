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

// BudgetSetInput sets the budget of a category for a month. Category is a UUID
// or name of an expense category; Month is "YYYY-MM" in WIB.
type BudgetSetInput struct {
	Space    string
	Category string
	Month    string
	Amount   int64
}

// BudgetDeleteInput removes the budget of a category for a month.
type BudgetDeleteInput struct {
	Space    string
	Category string
	Month    string
}

// BudgetStatusView is a budget status with the category name.
type BudgetStatusView struct {
	domainfinance.BudgetStatus
	CategoryName string `json:"category_name"`
}

// BalancesReport lists the balance of every non-archived account and their
// sum (credit card balances are negative and are included).
type BalancesReport struct {
	Accounts []domainfinance.AccountBalance `json:"accounts"`
	Total    int64                          `json:"total"`
}

// SummaryInput selects a report period: today, week (ISO, Monday start),
// month, last_month or custom, all in WIB; empty means month. From and To
// belong to custom: with an empty Period they imply it, with any other Period
// they are rejected.
type SummaryInput struct {
	Space  string
	Period string
	From   *time.Time
	To     *time.Time
}

// SummaryReport covers the half-open UTC range [From, To). Previous* is the
// immediately preceding period of the same kind. Net is Income - Expense;
// transfers count in neither.
type SummaryReport struct {
	Period          string                        `json:"period"`
	From            time.Time                     `json:"from"`
	To              time.Time                     `json:"to"`
	Income          int64                         `json:"income"`
	Expense         int64                         `json:"expense"`
	Net             int64                         `json:"net"`
	PreviousIncome  int64                         `json:"previous_income"`
	PreviousExpense int64                         `json:"previous_expense"`
	ByCategory      []domainfinance.CategoryTotal `json:"by_category"`
	ByAccount       []domainfinance.AccountTotal  `json:"by_account"`
	TopMerchants    []domainfinance.MerchantTotal `json:"top_merchants"`
}

// RecurringCreateInput sets up a recurring transaction template. Account,
// ToAccount and Category accept a UUID or an exact case-insensitive name.
// Frequency is daily, weekly or monthly; IntervalN defaults to 1 (1..365 for
// daily and weekly, 1..12 for monthly). DayOfMonth is for monthly only and
// defaults to StartAt's WIB day. The first occurrence is StartAt itself.
type RecurringCreateInput struct {
	Space      string
	Type       string
	Amount     int64
	Account    string
	ToAccount  string
	Category   string
	Merchant   string
	Note       string
	Frequency  string
	IntervalN  int
	DayOfMonth *int
	StartAt    time.Time
	EndAt      *time.Time
}

type RecurringListInput struct {
	Space string
	Limit int
}

// RecurringUpdateInput changes only the fields that are set. An empty string
// for ToAccount or Category clears it; ClearEndAt removes the end. Switching
// to daily or weekly drops the monthly day unless DayOfMonth is sent (which
// is then rejected). Paused pauses or resumes the template.
type RecurringUpdateInput struct {
	Space      string
	ID         string
	Type       *string
	Amount     *int64
	Account    *string
	ToAccount  *string
	Category   *string
	Merchant   *string
	Note       *string
	Frequency  *string
	IntervalN  *int
	DayOfMonth *int
	StartAt    *time.Time
	EndAt      *time.Time
	ClearEndAt bool
	Paused     *bool
}

type RecurringDeleteInput struct {
	Space string
	ID    string
}
