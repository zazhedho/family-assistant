package servicefinance

import (
	"context"
	"errors"
	"strings"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	"family-assistant/utils"

	"gorm.io/gorm"
)

const resourceRecurring = "finance_recurring"

// RecurringCreate stores a recurring transaction template. References resolve
// like a transaction's (archived ones rejected); the first occurrence, and so
// the initial next_run_at, is StartAt itself.
func (s *service) RecurringCreate(ctx context.Context, actor domainidentity.ActorContext, input dto.RecurringCreateInput) (rec *domainfinance.Recurring, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() {
		resourceID := ""
		if rec != nil {
			resourceID = rec.ID
		}
		s.auditResult(ctx, actor, domainaudit.ActionCreate, resourceRecurring, spaceID, resourceID, err)
	}()

	if err = s.configured(); err != nil {
		return nil, err
	}
	memberID, err := s.guardMember(ctx, actor, spaceID, permManage)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	r := domainfinance.Recurring{
		ID: utils.CreateUUID(), SpaceID: spaceID, Frequency: normalizeFrequency(input.Frequency), IntervalN: input.IntervalN,
		DayOfMonth: input.DayOfMonth, StartAt: input.StartAt.UTC(), EndAt: utcPtr(input.EndAt),
		CreatedByMemberID: memberID, CreatedAt: now, UpdatedAt: now,
	}
	if r.IntervalN == 0 {
		r.IntervalN = 1
	}
	if r.Frequency == domainfinance.FreqMonthly && r.DayOfMonth == nil && !r.StartAt.IsZero() {
		day := r.StartAt.In(domainfinance.WIB()).Day()
		r.DayOfMonth = &day
	}
	patch := dto.TransactionUpdateInput{
		Type: &input.Type, Amount: &input.Amount, Account: &input.Account, ToAccount: &input.ToAccount,
		Category: &input.Category, Merchant: &input.Merchant, Note: &input.Note,
	}
	if err = s.applyTemplate(ctx, &r, patch); err != nil {
		return nil, err
	}
	r.NextRunAt = r.StartAt
	if err = s.repo.CreateRecurring(ctx, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// RecurringList returns the Space's templates by next run, clamped to the list
// limit.
func (s *service) RecurringList(ctx context.Context, actor domainidentity.ActorContext, input dto.RecurringListInput) (recs []domainfinance.Recurring, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() { s.auditResult(ctx, actor, actionList, resourceRecurring, spaceID, "", err) }()

	if err = s.configured(); err != nil {
		return nil, err
	}
	limit, err := clampLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if err = s.guard(ctx, actor, spaceID, permList); err != nil {
		return nil, err
	}
	if recs, err = s.repo.ListRecurring(ctx, spaceID); err != nil {
		return nil, err
	}
	if len(recs) > limit {
		recs = recs[:limit]
	}
	return recs, nil
}

// RecurringUpdate changes the fields that are set and re-validates the merged
// template. Pausing and resuming change nothing else: a resumed template that
// fell behind is caught up by the scheduler's per-run cap. A schedule change
// (frequency, interval, day, start) re-chains next_run_at from StartAt but
// never before the occurrence that was due next, so nothing already posted is
// posted again. The write is rejected when the scheduler (or another edit)
// changed the template after it was loaded.
func (s *service) RecurringUpdate(ctx context.Context, actor domainidentity.ActorContext, input dto.RecurringUpdateInput) (rec *domainfinance.Recurring, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() { s.auditResult(ctx, actor, domainaudit.ActionUpdate, resourceRecurring, spaceID, input.ID, err) }()

	id, err := requireID(input.ID)
	if err != nil {
		return nil, err
	}
	if noChanges(input) {
		return nil, validationError("update", "at least one field is required")
	}
	if err = s.configured(); err != nil {
		return nil, err
	}
	if err = s.guard(ctx, actor, spaceID, permManage); err != nil {
		return nil, err
	}
	cur, err := s.repo.FindRecurring(ctx, spaceID, id)
	if err != nil {
		return nil, notFound(err)
	}
	version, before := cur.UpdatedAt, *cur
	applySchedule(cur, input)
	patch := dto.TransactionUpdateInput{
		Type: input.Type, Amount: input.Amount, Account: input.Account, ToAccount: input.ToAccount,
		Category: input.Category, Merchant: input.Merchant, Note: input.Note,
	}
	if err = s.applyTemplate(ctx, cur, patch); err != nil {
		return nil, err
	}
	if scheduleChanged(before, *cur) {
		cur.NextRunAt = firstOccurrenceFrom(*cur, before.NextRunAt)
	}
	if err = s.repo.UpdateRecurring(ctx, cur, version); err != nil {
		return nil, s.staleOrMissing(ctx, spaceID, id, err)
	}
	return cur, nil
}

// RecurringDelete soft-deletes a template; posted transactions stay.
func (s *service) RecurringDelete(ctx context.Context, actor domainidentity.ActorContext, input dto.RecurringDeleteInput) (err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() { s.auditResult(ctx, actor, domainaudit.ActionDelete, resourceRecurring, spaceID, input.ID, err) }()

	id, err := requireID(input.ID)
	if err != nil {
		return err
	}
	if err = s.configured(); err != nil {
		return err
	}
	if err = s.guard(ctx, actor, spaceID, permManage); err != nil {
		return err
	}
	return notFound(s.repo.DeleteRecurring(ctx, spaceID, id, s.now().UTC()))
}

// applyTemplate merges the transaction fields of patch into r through the
// transaction rules (reference resolution, kind check, validation), then
// validates the whole template.
func (s *service) applyTemplate(ctx context.Context, r *domainfinance.Recurring, patch dto.TransactionUpdateInput) error {
	tx := r.TransactionAt(r.StartAt, r.ID, time.Time{})
	if err := s.applyPatch(ctx, &tx, patch); err != nil {
		return err
	}
	r.Type, r.Amount, r.AccountID, r.ToAccountID, r.CategoryID = tx.Type, tx.Amount, tx.AccountID, tx.ToAccountID, tx.CategoryID
	r.Merchant, r.Note = tx.Merchant, tx.Note
	return mapDomainError(domainfinance.ValidateRecurring(*r))
}

// applySchedule merges the schedule fields of input into r.
func applySchedule(r *domainfinance.Recurring, input dto.RecurringUpdateInput) {
	if input.Frequency != nil {
		r.Frequency = normalizeFrequency(*input.Frequency)
		if r.Frequency != domainfinance.FreqMonthly && input.DayOfMonth == nil {
			r.DayOfMonth = nil
		}
	}
	if input.IntervalN != nil {
		r.IntervalN = *input.IntervalN
	}
	if input.DayOfMonth != nil {
		day := *input.DayOfMonth
		r.DayOfMonth = &day
	}
	if input.StartAt != nil {
		r.StartAt = input.StartAt.UTC()
	}
	if input.EndAt != nil {
		r.EndAt = utcPtr(input.EndAt)
	}
	if input.ClearEndAt {
		r.EndAt = nil
	}
	if input.Paused != nil {
		r.Paused = *input.Paused
	}
}

func scheduleChanged(a, b domainfinance.Recurring) bool {
	dayA, dayB := 0, 0
	if a.DayOfMonth != nil {
		dayA = *a.DayOfMonth
	}
	if b.DayOfMonth != nil {
		dayB = *b.DayOfMonth
	}
	return a.Frequency != b.Frequency || a.IntervalN != b.IntervalN || dayA != dayB || !a.StartAt.Equal(b.StartAt)
}

// firstOccurrenceFrom returns the first occurrence of r (chained from StartAt)
// at or after notBefore.
func firstOccurrenceFrom(r domainfinance.Recurring, notBefore time.Time) time.Time {
	occurrence := r.StartAt
	for occurrence.Before(notBefore) {
		next := domainfinance.NextOccurrence(r, occurrence)
		if !next.After(occurrence) {
			break
		}
		occurrence = next
	}
	return occurrence
}

// staleOrMissing maps a failed versioned update: a row that still exists was
// changed after it was loaded, anything else is not found.
func (s *service) staleOrMissing(ctx context.Context, spaceID, id string, err error) error {
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if _, findErr := s.repo.FindRecurring(ctx, spaceID, id); findErr == nil {
		return validationError("id", "was changed concurrently; retry")
	}
	return notFound(err)
}

// noChanges reports an update that sets no field besides Space and ID.
func noChanges(input dto.RecurringUpdateInput) bool {
	input.Space, input.ID = "", ""
	return input == dto.RecurringUpdateInput{}
}

func normalizeFrequency(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
