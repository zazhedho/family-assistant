package domainfinance

import (
	"errors"
	"time"
)

// Recurring schedule limits.
const (
	MaxIntervalDays   = 365 // daily and weekly interval_n
	MaxIntervalMonths = 12  // monthly interval_n
)

// Sentinel errors of ValidateRecurring; the service maps them to field errors.
var (
	ErrInvalidFrequency     = errors.New("frequency must be daily, weekly or monthly")
	ErrIntervalRange        = errors.New("interval_n must be 1..365 for daily and weekly, 1..12 for monthly")
	ErrDayOfMonthRange      = errors.New("day_of_month must be between 1 and 31")
	ErrDayOfMonthNotMonthly = errors.New("day_of_month is only allowed for a monthly schedule")
	ErrStartRequired        = errors.New("start_at is required")
	ErrEndBeforeStart       = errors.New("end_at must not be before start_at")
)

// ValidateRecurring checks the transaction template (the same rules as
// ValidateTransaction) and the schedule. Like ValidateTransaction it does not
// check Space membership, references or archived state.
func ValidateRecurring(r Recurring) error {
	if err := ValidateTransaction(r.TransactionAt(r.StartAt, "", time.Time{})); err != nil {
		return err
	}
	maxInterval := MaxIntervalDays
	switch r.Frequency {
	case FreqDaily, FreqWeekly:
		if r.DayOfMonth != nil {
			return ErrDayOfMonthNotMonthly
		}
	case FreqMonthly:
		maxInterval = MaxIntervalMonths
		if r.DayOfMonth != nil && (*r.DayOfMonth < 1 || *r.DayOfMonth > 31) {
			return ErrDayOfMonthRange
		}
	default:
		return ErrInvalidFrequency
	}
	if r.IntervalN < 1 || r.IntervalN > maxInterval {
		return ErrIntervalRange
	}
	if r.StartAt.IsZero() {
		return ErrStartRequired
	}
	if r.EndAt != nil && r.EndAt.Before(r.StartAt) {
		return ErrEndBeforeStart
	}
	return nil
}

// FirstOccurrence is the first occurrence of r, which is where next_run_at
// starts. Daily and weekly schedules, and a monthly one without DayOfMonth,
// start at StartAt. A monthly schedule with DayOfMonth starts on the first
// WIB date on or after StartAt whose day is DayOfMonth (clamped to the
// month's length, so 31 means the last day), at StartAt's WIB clock time.
func FirstOccurrence(r Recurring) time.Time {
	if r.Frequency != FreqMonthly || r.DayOfMonth == nil {
		return r.StartAt
	}
	w := r.StartAt.In(WIB())
	for _, add := range []int{0, 1} {
		total := w.Year()*12 + int(w.Month()) - 1 + add
		year, month := total/12, time.Month(total%12+1)
		day := *r.DayOfMonth
		if last := daysIn(year, month); day > last {
			day = last
		}
		candidate := time.Date(year, month, day, w.Hour(), w.Minute(), w.Second(), w.Nanosecond(), WIB())
		if !candidate.Before(w) {
			return candidate.UTC()
		}
	}
	return r.StartAt // unreachable: next month's candidate is always later
}

// TransactionAt is the transaction the template posts for one occurrence:
// the template's type, amount, accounts, category, merchant and note, dated at
// the occurrence and attributed to the template's creator. The optional ids
// are copied, so the result never aliases the template.
func (r Recurring) TransactionAt(occurrence time.Time, id string, now time.Time) Transaction {
	return Transaction{
		ID: id, SpaceID: r.SpaceID, Type: r.Type, Amount: r.Amount, AccountID: r.AccountID,
		ToAccountID: copyID(r.ToAccountID), CategoryID: copyID(r.CategoryID), Merchant: r.Merchant, Note: r.Note,
		OccurredAt: occurrence.UTC(), CreatedByMemberID: r.CreatedByMemberID, CreatedAt: now, UpdatedAt: now,
	}
}

func copyID(v *string) *string {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// Reason codes a scheduler run reports, and stores in audit, for a template it
// paused or skipped. They are fixed strings: raw error text is never stored.
const (
	PauseSpaceInvalid     = "space_invalid"     // Space gone or no longer a finance Space
	PauseMemberInvalid    = "member_invalid"    // creator no longer an active member
	PauseReferenceInvalid = "reference_invalid" // account/category missing, archived or of the wrong kind
	PauseTemplateInvalid  = "template_invalid"  // the stored template fails ValidateRecurring
	PauseValidationFailed = "validation_failed" // the built transaction fails validation
	PauseInsertRejected   = "insert_rejected"   // the database rejected the row (constraint or data error)
	PauseScheduleInvalid  = "schedule_invalid"  // the schedule does not advance
	PauseRetryExhausted   = "retry_exhausted"   // MaxConsecutiveFailures transient failures in a row
	SkipTransient         = "transient"         // rolled back, retried on the next run
)

// MaxConsecutiveFailures (30, about half an hour at one run a minute) is how many runs in a row may fail a template with an
// unclassified (transient) error before it is paused with PauseRetryExhausted.
const MaxConsecutiveFailures = 30

// PauseError makes the scheduler pause a template with Reason. Any other error
// from a build callback is treated as transient: the template is rolled back
// and retried on the next run.
type PauseError struct{ Reason string }

func (e *PauseError) Error() string { return "recurring template paused: " + e.Reason }

// Pause returns a *PauseError with reason.
func Pause(reason string) error { return &PauseError{Reason: reason} }

// CheckRecurringReferences reports PauseReferenceInvalid unless every account
// the template uses is among accounts (the live rows of its Space) and not
// archived, and its category, when set, is category, not archived, and of the
// template's type.
func CheckRecurringReferences(r Recurring, accounts []Account, category *Category) error {
	ids := []string{r.AccountID}
	if r.ToAccountID != nil {
		ids = append(ids, *r.ToAccountID)
	}
	for _, id := range ids {
		if !liveAccount(accounts, id) {
			return Pause(PauseReferenceInvalid)
		}
	}
	if r.CategoryID == nil {
		return nil
	}
	if category == nil || category.ID != *r.CategoryID || category.ArchivedAt != nil || category.Kind != r.Type {
		return Pause(PauseReferenceInvalid)
	}
	return nil
}

func liveAccount(accounts []Account, id string) bool {
	for _, a := range accounts {
		if a.ID == id {
			return a.ArchivedAt == nil
		}
	}
	return false
}

// RecurringBuildFunc builds the transaction a template posts for one
// occurrence. A *PauseError pauses the template; any other error skips it for
// this run.
type RecurringBuildFunc func(r Recurring, occurrence time.Time) (Transaction, error)

// PostedOccurrence is one transaction a scheduler run posted.
type PostedOccurrence struct {
	RecurringID   string
	SpaceID       string
	TransactionID string
	MemberID      string // the template's creator, who the transaction is attributed to
	OccurredAt    time.Time
}

// RecurringIssue is a template a run paused or skipped, with a reason code
// and, when an error caused it, its sanitized failure code.
type RecurringIssue struct {
	RecurringID string
	SpaceID     string
	MemberID    string
	Reason      string
	Code        string
}

// PostResult is the outcome of one committed scheduler run. Dropped counts,
// per template id, the missed occurrences skipped after the catch-up cap;
// AlreadyPosted counts occurrences found already posted and passed over.
type PostResult struct {
	Posted        []PostedOccurrence
	Paused        []RecurringIssue
	Skipped       []RecurringIssue
	Dropped       map[string]int
	AlreadyPosted int
}
