package repositoryfinance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/utils"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrBuildRequired is returned by PostDueRecurring without a build callback.
var ErrBuildRequired = errors.New("finance: recurring build callback is required")

const (
	recurringSavepoint = "recurring_post"
	maxFailureCodeLen  = 32 // finance_recurring.last_failure_code VARCHAR(32)
)

// PostDueRecurring posts the due occurrences of up to limit templates in ONE
// database transaction, so a crash at any point either keeps both the posted
// transactions and the advanced next_run_at or neither:
//
//   - it selects due templates (not paused, not deleted, next_run_at <= now and
//     next_run_at within end_at) with FOR UPDATE SKIP LOCKED, so an
//     overlapping run on another instance skips them until this one commits
//     and then sees their advanced next_run_at: an occurrence never posts twice.
//     They are ordered by failure_count first, so templates that keep failing
//     go last and cannot starve the others out of the batch;
//   - per template it posts each due occurrence up to maxPerTemplate (build,
//     ValidateTransaction, insert tagged with recurring_id, then chain with
//     NextOccurrence), stops at end_at, and after the cap fast-forwards
//     next_run_at to the first occurrence after now, counting the dropped
//     ones in Dropped. An occurrence the unique (recurring_id, occurred_at)
//     index already holds is passed over (AlreadyPosted), not posted again;
//   - each template runs under a SAVEPOINT, released on success, where the
//     advance also resets failure_count and last_failure_code. On a
//     *PauseError (from build or from the checks here) its writes are rolled
//     back and it is paused with pause_reason. On any other error it is
//     rolled back, failure_count goes up and last_failure_code records a
//     sanitized code; at MaxConsecutiveFailures it is paused with
//     retry_exhausted, otherwise it is reported in Skipped and retried by the
//     next run. Other templates are not affected.
//
// A database error outside that per-template handling rolls everything back
// and is returned with an empty result. The result describes the committed
// state only.
func (r *Repository) PostDueRecurring(ctx context.Context, now time.Time, limit, maxPerTemplate int, build domainfinance.RecurringBuildFunc) (domainfinance.PostResult, error) {
	result := domainfinance.PostResult{Dropped: map[string]int{}}
	if build == nil {
		return result, ErrBuildRequired
	}
	if limit <= 0 || maxPerTemplate <= 0 {
		return result, nil
	}
	err := r.db(ctx).Transaction(func(tx *gorm.DB) error {
		var due []domainfinance.Recurring
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("paused = ? AND next_run_at <= ? AND (end_at IS NULL OR next_run_at <= end_at)", false, now).
			Order("failure_count, next_run_at, id").Limit(limit).Find(&due).Error; err != nil {
			return err
		}
		for i := range due {
			if err := postTemplate(tx, due[i], now, maxPerTemplate, build, &result); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domainfinance.PostResult{Dropped: map[string]int{}}, err
	}
	return result, nil
}

// templateRun is what one template's posting produced.
type templateRun struct {
	posted        []domainfinance.PostedOccurrence
	dropped       int
	alreadyPosted int
}

// postTemplate runs one template under the savepoint and records its outcome
// in result. It returns only errors that must abort the whole run.
func postTemplate(tx *gorm.DB, rec domainfinance.Recurring, now time.Time, maxPerTemplate int, build domainfinance.RecurringBuildFunc, result *domainfinance.PostResult) error {
	if err := tx.Exec("SAVEPOINT " + recurringSavepoint).Error; err != nil {
		return err
	}
	run, err := postOccurrences(tx, rec, now, maxPerTemplate, build)
	if err == nil {
		if err := tx.Exec("RELEASE SAVEPOINT " + recurringSavepoint).Error; err != nil {
			return err
		}
		result.Posted = append(result.Posted, run.posted...)
		result.AlreadyPosted += run.alreadyPosted
		if run.dropped > 0 {
			result.Dropped[rec.ID] = run.dropped
		}
		return nil
	}
	var fatal errFatal
	if errors.As(err, &fatal) {
		return err
	}
	if rbErr := tx.Exec("ROLLBACK TO SAVEPOINT " + recurringSavepoint).Error; rbErr != nil {
		return rbErr
	}
	return recordFailure(tx, rec, now, err, result)
}

// recordFailure pauses or counts a failed template in the outer transaction.
func recordFailure(tx *gorm.DB, rec domainfinance.Recurring, now time.Time, err error, result *domainfinance.PostResult) error {
	issue := domainfinance.RecurringIssue{RecurringID: rec.ID, SpaceID: rec.SpaceID, MemberID: rec.CreatedByMemberID}
	updates := map[string]any{"updated_at": now}
	var pauseErr *domainfinance.PauseError
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pauseErr):
		issue.Reason = pauseErr.Reason
		if errors.As(err, &pgErr) {
			issue.Code = failureCode(err)
		}
		updates["paused"], updates["pause_reason"] = true, issue.Reason
	default:
		issue.Code = failureCode(err)
		count := rec.FailureCount + 1
		updates["failure_count"], updates["last_failure_code"] = count, issue.Code
		issue.Reason = domainfinance.SkipTransient
		if count >= domainfinance.MaxConsecutiveFailures {
			issue.Reason = domainfinance.PauseRetryExhausted
			updates["paused"], updates["pause_reason"] = true, issue.Reason
		}
	}
	if err := updateScoped(tx, &domainfinance.Recurring{}, rec.SpaceID, rec.ID, updates); err != nil {
		return err
	}
	if issue.Reason == domainfinance.SkipTransient {
		result.Skipped = append(result.Skipped, issue)
	} else {
		result.Paused = append(result.Paused, issue)
	}
	return nil
}

// errFatal marks an error that aborts the run (a failed advance).
type errFatal struct{ err error }

func (e errFatal) Error() string { return e.err.Error() }
func (e errFatal) Unwrap() error { return e.err }

// postOccurrences posts the due occurrences of rec and advances next_run_at.
func postOccurrences(tx *gorm.DB, rec domainfinance.Recurring, now time.Time, maxPerTemplate int, build domainfinance.RecurringBuildFunc) (templateRun, error) {
	var run templateRun
	if domainfinance.ValidateRecurring(rec) != nil {
		return run, domainfinance.Pause(domainfinance.PauseTemplateInvalid)
	}
	if err := checkReferences(tx, rec); err != nil {
		return run, err
	}
	occurrence := rec.NextRunAt
	var err error
	for handled := 0; handled < maxPerTemplate && isDue(rec, occurrence, now); handled++ {
		p, inserted, postErr := postOne(tx, rec, occurrence, build)
		if postErr != nil {
			return templateRun{}, postErr
		}
		if inserted {
			run.posted = append(run.posted, p)
		} else {
			run.alreadyPosted++
		}
		if occurrence, err = advance(rec, occurrence); err != nil {
			return templateRun{}, err
		}
	}
	for isDue(rec, occurrence, now) {
		run.dropped++
		if occurrence, err = advance(rec, occurrence); err != nil {
			return templateRun{}, err
		}
	}
	if err := updateScoped(tx, &domainfinance.Recurring{}, rec.SpaceID, rec.ID, map[string]any{
		"next_run_at": occurrence, "failure_count": 0, "last_failure_code": nil, "updated_at": now,
	}); err != nil {
		return templateRun{}, errFatal{err}
	}
	return run, nil
}

// postOne builds, checks and inserts one occurrence. inserted is false when
// the unique (recurring_id, occurred_at) index already holds it.
func postOne(tx *gorm.DB, rec domainfinance.Recurring, occurrence time.Time, build domainfinance.RecurringBuildFunc) (domainfinance.PostedOccurrence, bool, error) {
	t, err := build(rec, occurrence)
	if err != nil {
		return domainfinance.PostedOccurrence{}, false, err
	}
	// The built row must be the template's own occurrence in its own Space,
	// attributed to its creator.
	if t.SpaceID != rec.SpaceID || t.CreatedByMemberID != rec.CreatedByMemberID || !t.OccurredAt.Equal(occurrence) ||
		domainfinance.ValidateTransaction(t) != nil {
		return domainfinance.PostedOccurrence{}, false, domainfinance.Pause(domainfinance.PauseValidationFailed)
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = utils.CreateUUID()
	}
	recurringID := rec.ID
	t.RecurringID = &recurringID
	result := tx.Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "recurring_id"}, {Name: "occurred_at"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "recurring_id IS NOT NULL"}}},
		DoNothing:   true,
	}).Create(&t)
	if result.Error != nil {
		if isDataError(result.Error) {
			return domainfinance.PostedOccurrence{}, false, fmt.Errorf("%w: %w", domainfinance.Pause(domainfinance.PauseInsertRejected), result.Error)
		}
		return domainfinance.PostedOccurrence{}, false, result.Error
	}
	if result.RowsAffected == 0 {
		return domainfinance.PostedOccurrence{}, false, nil
	}
	return domainfinance.PostedOccurrence{
		RecurringID: rec.ID, SpaceID: rec.SpaceID, TransactionID: t.ID, MemberID: t.CreatedByMemberID, OccurredAt: t.OccurredAt,
	}, true, nil
}

// isDue reports whether occurrence is at or before now and within end_at.
func isDue(rec domainfinance.Recurring, occurrence, now time.Time) bool {
	return !occurrence.After(now) && (rec.EndAt == nil || !occurrence.After(*rec.EndAt))
}

// advance returns the next occurrence, pausing a schedule that would not move
// forward (which would otherwise loop forever).
func advance(rec domainfinance.Recurring, occurrence time.Time) (time.Time, error) {
	next := domainfinance.NextOccurrence(rec, occurrence)
	if !next.After(occurrence) {
		return occurrence, domainfinance.Pause(domainfinance.PauseScheduleInvalid)
	}
	return next, nil
}

// checkReferences loads the template's accounts and category inside the
// posting transaction and applies CheckRecurringReferences.
func checkReferences(tx *gorm.DB, rec domainfinance.Recurring) error {
	ids := []string{rec.AccountID}
	if rec.ToAccountID != nil {
		ids = append(ids, *rec.ToAccountID)
	}
	var accounts []domainfinance.Account
	if err := tx.Where("space_id = ? AND id IN ?", rec.SpaceID, ids).Find(&accounts).Error; err != nil {
		return err
	}
	var category *domainfinance.Category
	if rec.CategoryID != nil {
		var found []domainfinance.Category
		if err := tx.Where("space_id = ? AND id = ?", rec.SpaceID, *rec.CategoryID).Find(&found).Error; err != nil {
			return err
		}
		if len(found) > 0 {
			category = &found[0]
		}
	}
	return domainfinance.CheckRecurringReferences(rec, accounts, category)
}

// isDataError reports a Postgres data exception (class 22) or integrity
// constraint violation (class 23): the row itself is bad, retrying won't help.
func isDataError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "22") || strings.HasPrefix(pgErr.Code, "23")
	}
	return errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, gorm.ErrForeignKeyViolated) ||
		errors.Is(err, gorm.ErrCheckConstraintViolated)
}

// failureCode turns an error into a code safe to store and log: the Postgres
// SQLSTATE, a context class, or the Go type of the innermost error. Error
// text, which may hold values or connection details, is never used.
func failureCode(err error) string {
	var pgErr *pgconn.PgError
	code := ""
	switch {
	case errors.As(err, &pgErr):
		code = "SQLSTATE_" + pgErr.Code
	case errors.Is(err, context.Canceled):
		code = "CONTEXT_CANCELED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "CONTEXT_DEADLINE"
	default:
		for next := errors.Unwrap(err); next != nil; next = errors.Unwrap(err) {
			err = next
		}
		code = "GO_" + strings.TrimLeft(fmt.Sprintf("%T", err), "*")
	}
	if len(code) > maxFailureCodeLen {
		code = code[:maxFailureCodeLen]
	}
	return code
}
