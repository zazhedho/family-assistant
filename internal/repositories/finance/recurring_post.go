package repositoryfinance

import (
	"context"
	"errors"
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

const recurringSavepoint = "recurring_post"

// PostDueRecurring posts the due occurrences of up to limit templates in ONE
// database transaction, so a crash at any point either keeps both the posted
// transactions and the advanced next_run_at or neither:
//
//   - it selects due templates (not paused, not deleted, next_run_at <= now and
//     next_run_at within end_at) with FOR UPDATE SKIP LOCKED, so an
//     overlapping run on another instance skips them until this one commits
//     and then sees their advanced next_run_at: an occurrence never posts twice;
//   - per template it posts each due occurrence up to maxPerTemplate (build,
//     ValidateTransaction, insert, then chain with NextOccurrence), stops at
//     end_at, and after the cap fast-forwards next_run_at to the first
//     occurrence after now, counting the dropped ones in Dropped;
//   - each template runs under a SAVEPOINT: on a *PauseError (from build or
//     from the checks here) its writes are rolled back and it is marked paused
//     with a reason code; on any other build error it is rolled back and
//     reported in Skipped, to be retried by the next run. Other templates
//     are not affected.
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
			Order("next_run_at, id").Limit(limit).Find(&due).Error; err != nil {
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

// postTemplate runs one template under the savepoint and records its outcome
// in result. It returns only errors that must abort the whole run.
func postTemplate(tx *gorm.DB, rec domainfinance.Recurring, now time.Time, maxPerTemplate int, build domainfinance.RecurringBuildFunc, result *domainfinance.PostResult) error {
	if err := tx.Exec("SAVEPOINT " + recurringSavepoint).Error; err != nil {
		return err
	}
	posted, dropped, err := postOccurrences(tx, rec, now, maxPerTemplate, build)
	if err == nil {
		result.Posted = append(result.Posted, posted...)
		if dropped > 0 {
			result.Dropped[rec.ID] = dropped
		}
		return nil
	}
	reason, pause := failureReason(err)
	if reason == "" {
		return err
	}
	if rbErr := tx.Exec("ROLLBACK TO SAVEPOINT " + recurringSavepoint).Error; rbErr != nil {
		return rbErr
	}
	issue := domainfinance.RecurringIssue{RecurringID: rec.ID, SpaceID: rec.SpaceID, MemberID: rec.CreatedByMemberID, Reason: reason}
	if !pause {
		result.Skipped = append(result.Skipped, issue)
		return nil
	}
	if err := updateScoped(tx, &domainfinance.Recurring{}, rec.SpaceID, rec.ID, map[string]any{"paused": true, "updated_at": now}); err != nil {
		return err
	}
	result.Paused = append(result.Paused, issue)
	return nil
}

// errFatal marks an error that aborts the run (a failed advance).
type errFatal struct{ err error }

func (e errFatal) Error() string { return e.err.Error() }
func (e errFatal) Unwrap() error { return e.err }

// failureReason classifies a template failure: a pause reason, the transient
// skip reason, or "" for an error that must abort the run.
func failureReason(err error) (reason string, pause bool) {
	var pauseErr *domainfinance.PauseError
	var fatal errFatal
	switch {
	case errors.As(err, &fatal):
		return "", false
	case errors.As(err, &pauseErr):
		return pauseErr.Reason, true
	default:
		return domainfinance.SkipTransient, false
	}
}

// postOccurrences posts the due occurrences of rec and advances next_run_at.
func postOccurrences(tx *gorm.DB, rec domainfinance.Recurring, now time.Time, maxPerTemplate int, build domainfinance.RecurringBuildFunc) ([]domainfinance.PostedOccurrence, int, error) {
	if domainfinance.ValidateRecurring(rec) != nil {
		return nil, 0, domainfinance.Pause(domainfinance.PauseTemplateInvalid)
	}
	if err := checkReferences(tx, rec); err != nil {
		return nil, 0, err
	}
	var posted []domainfinance.PostedOccurrence
	occurrence := rec.NextRunAt
	var err error
	for len(posted) < maxPerTemplate && isDue(rec, occurrence, now) {
		p, postErr := postOne(tx, rec, occurrence, build)
		if postErr != nil {
			return nil, 0, postErr
		}
		posted = append(posted, p)
		if occurrence, err = advance(rec, occurrence); err != nil {
			return nil, 0, err
		}
	}
	dropped := 0
	for isDue(rec, occurrence, now) {
		dropped++
		if occurrence, err = advance(rec, occurrence); err != nil {
			return nil, 0, err
		}
	}
	if err := updateScoped(tx, &domainfinance.Recurring{}, rec.SpaceID, rec.ID,
		map[string]any{"next_run_at": occurrence, "updated_at": now}); err != nil {
		return nil, 0, errFatal{err}
	}
	return posted, dropped, nil
}

func postOne(tx *gorm.DB, rec domainfinance.Recurring, occurrence time.Time, build domainfinance.RecurringBuildFunc) (domainfinance.PostedOccurrence, error) {
	t, err := build(rec, occurrence)
	if err != nil {
		return domainfinance.PostedOccurrence{}, err
	}
	// The built row must be the template's own occurrence in its own Space,
	// attributed to its creator.
	if t.SpaceID != rec.SpaceID || t.CreatedByMemberID != rec.CreatedByMemberID || !t.OccurredAt.Equal(occurrence) ||
		domainfinance.ValidateTransaction(t) != nil {
		return domainfinance.PostedOccurrence{}, domainfinance.Pause(domainfinance.PauseValidationFailed)
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = utils.CreateUUID()
	}
	if err := tx.Create(&t).Error; err != nil {
		if isDataError(err) {
			return domainfinance.PostedOccurrence{}, domainfinance.Pause(domainfinance.PauseInsertRejected)
		}
		return domainfinance.PostedOccurrence{}, err
	}
	return domainfinance.PostedOccurrence{
		RecurringID: rec.ID, SpaceID: rec.SpaceID, TransactionID: t.ID, MemberID: t.CreatedByMemberID, OccurredAt: t.OccurredAt,
	}, nil
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
