package domainfinance

import (
	"time"

	"gorm.io/gorm"
)

const (
	TypeIncome   = "income"
	TypeExpense  = "expense"
	TypeTransfer = "transfer"
)

const (
	FreqDaily   = "daily"
	FreqWeekly  = "weekly"
	FreqMonthly = "monthly"
	FreqYearly  = "yearly"
)

func (Account) TableName() string     { return "finance_accounts" }
func (Category) TableName() string    { return "finance_categories" }
func (Transaction) TableName() string { return "finance_transactions" }
func (Budget) TableName() string      { return "finance_budgets" }
func (Recurring) TableName() string   { return "finance_recurring" }

type Account struct {
	ID             string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID        string         `json:"space_id" gorm:"column:space_id"`
	Name           string         `json:"name" gorm:"column:name"`
	Type           string         `json:"type" gorm:"column:type"`
	OpeningBalance Money          `json:"opening_balance" gorm:"column:opening_balance"`
	ArchivedAt     *time.Time     `json:"archived_at,omitempty" gorm:"column:archived_at"`
	CreatedAt      time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt      time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

type Category struct {
	ID         string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID    string         `json:"space_id" gorm:"column:space_id"`
	Name       string         `json:"name" gorm:"column:name"`
	Kind       string         `json:"kind" gorm:"column:kind"`
	ArchivedAt *time.Time     `json:"archived_at,omitempty" gorm:"column:archived_at"`
	CreatedAt  time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt  time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt  gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

// Transaction. ToAccountID and CategoryID are nullable UUID columns: nil means
// SQL NULL, and an empty string must never be written.
type Transaction struct {
	ID                string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID           string         `json:"space_id" gorm:"column:space_id"`
	Type              string         `json:"type" gorm:"column:type"`
	Amount            Money          `json:"amount" gorm:"column:amount"`
	AccountID         string         `json:"account_id" gorm:"column:account_id"`
	ToAccountID       *string        `json:"to_account_id,omitempty" gorm:"column:to_account_id"`
	CategoryID        *string        `json:"category_id,omitempty" gorm:"column:category_id"`
	Merchant          string         `json:"merchant,omitempty" gorm:"column:merchant"`
	Note              string         `json:"note,omitempty" gorm:"column:note"`
	OccurredAt        time.Time      `json:"occurred_at" gorm:"column:occurred_at"`
	CreatedByMemberID string         `json:"created_by_member_id" gorm:"column:created_by_member_id"`
	CreatedAt         time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt         gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
	// RecurringID is the template that posted the row (nil for manual
	// entries); (recurring_id, occurred_at) is unique, so an occurrence can
	// never be posted twice.
	RecurringID *string `json:"recurring_id,omitempty" gorm:"column:recurring_id"`
}

type Budget struct {
	ID         string `json:"id" gorm:"column:id;primaryKey"`
	SpaceID    string `json:"space_id" gorm:"column:space_id"`
	CategoryID string `json:"category_id" gorm:"column:category_id"`
	// Month is the DATE to store: use MonthDate (first day of the WIB month at
	// 00:00 UTC), never MonthStart, whose UTC instant breaks the first-day check.
	Month     time.Time      `json:"month" gorm:"column:month;type:date"`
	Amount    Money          `json:"amount" gorm:"column:amount"`
	CreatedAt time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

// Recurring. ToAccountID and CategoryID are nullable UUID columns: nil means
// SQL NULL, and an empty string must never be written.
type Recurring struct {
	ID          string     `json:"id" gorm:"column:id;primaryKey"`
	SpaceID     string     `json:"space_id" gorm:"column:space_id"`
	Type        string     `json:"type" gorm:"column:type"`
	Amount      Money      `json:"amount" gorm:"column:amount"`
	AccountID   string     `json:"account_id" gorm:"column:account_id"`
	ToAccountID *string    `json:"to_account_id,omitempty" gorm:"column:to_account_id"`
	CategoryID  *string    `json:"category_id,omitempty" gorm:"column:category_id"`
	Merchant    string     `json:"merchant,omitempty" gorm:"column:merchant"`
	Note        string     `json:"note,omitempty" gorm:"column:note"`
	Frequency   string     `json:"frequency" gorm:"column:frequency"`
	IntervalN   int        `json:"interval_n" gorm:"column:interval_n"`
	DayOfMonth  *int       `json:"day_of_month,omitempty" gorm:"column:day_of_month"`
	StartAt     time.Time  `json:"start_at" gorm:"column:start_at"`
	NextRunAt   time.Time  `json:"next_run_at" gorm:"column:next_run_at"`
	EndAt       *time.Time `json:"end_at,omitempty" gorm:"column:end_at"`
	Paused      bool       `json:"paused" gorm:"column:paused"`
	// PauseReason is the scheduler's reason code when it paused the template
	// (nil when running or paused by a user). FailureCount counts consecutive
	// unclassified posting failures and LastFailureCode is the sanitized code
	// of the last one (an SQLSTATE or an error class, never error text).
	PauseReason       *string        `json:"pause_reason,omitempty" gorm:"column:pause_reason"`
	FailureCount      int            `json:"failure_count" gorm:"column:failure_count"`
	LastFailureCode   *string        `json:"last_failure_code,omitempty" gorm:"column:last_failure_code"`
	CreatedByMemberID string         `json:"created_by_member_id" gorm:"column:created_by_member_id"`
	CreatedAt         time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt         gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

// ListFilter filters transaction listings. Limit is normalized by the service.
type ListFilter struct {
	SpaceID    string
	Type       string
	CategoryID string
	AccountID  string
	Search     string
	From       *time.Time
	To         *time.Time
	Limit      int
}

type AccountBalance struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Balance   Money  `json:"balance"`
}

type CategoryTotal struct {
	CategoryID string `json:"category_id"`
	Name       string `json:"name"`
	Total      Money  `json:"total"`
}

type AccountTotal struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	Income    Money  `json:"income"`
	Expense   Money  `json:"expense"`
}

type MerchantTotal struct {
	Merchant string `json:"merchant"`
	Total    Money  `json:"total"`
	Count    int    `json:"count"`
}

type SummaryResult struct {
	Income       Money           `json:"income"`
	Expense      Money           `json:"expense"`
	ByCategory   []CategoryTotal `json:"by_category"`
	ByAccount    []AccountTotal  `json:"by_account"`
	TopMerchants []MerchantTotal `json:"top_merchants"`
}

type BudgetStatus struct {
	CategoryID  string `json:"category_id"`
	Budget      Money  `json:"budget"`
	Spent       Money  `json:"spent"`
	Remaining   Money  `json:"remaining"`
	PercentUsed int    `json:"percent_used"`
}

// NewBudgetStatus computes remaining (negative when over budget) and the
// integer percent used, floor(spent*100/budget), 0 when budget is 0. The
// percent is computed without multiplying spent by 100 first, so it cannot
// overflow int64 for any input.
func NewBudgetStatus(categoryID string, budget, spent Money) BudgetStatus {
	s := BudgetStatus{CategoryID: categoryID, Budget: budget, Spent: spent, Remaining: budget - spent}
	if budget > 0 && spent > 0 {
		pct := spent/budget*100 + (spent%budget)*100/budget
		s.PercentUsed = int(pct)
	}
	return s
}
