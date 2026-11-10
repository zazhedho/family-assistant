package servicefinance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainspace "family-assistant/internal/domain/space"
	interfacefinance "family-assistant/internal/interfaces/finance"
	"family-assistant/pkg/logger"
	"family-assistant/utils"
)

const (
	// MaxCatchUp is how many missed occurrences of one template a run posts;
	// older ones are dropped and next_run_at moves past now.
	MaxCatchUp = 12

	defaultSchedulerInterval = time.Minute
	schedulerBatch           = 100 // templates per run
	auditSourceScheduler     = "scheduler"
)

type schedulerMembers interface {
	ListActiveMembers(ctx context.Context, spaceID string) ([]domainspace.ResolvedMembership, error)
}

// Scheduler posts due recurring transactions. All posting happens inside
// repo.PostDueRecurring (one transaction with row locks), so several
// instances may run at once without posting an occurrence twice.
type Scheduler struct {
	repo     interfacefinance.RepoFinanceInterface
	members  schedulerMembers
	audit    auditStore
	interval time.Duration
	now      func() time.Time
}

// NewScheduler builds a scheduler that ticks every interval (60s when not
// positive); now defaults to time.Now.
func NewScheduler(repo interfacefinance.RepoFinanceInterface, members schedulerMembers, audit auditStore, interval time.Duration, now func() time.Time) *Scheduler {
	if interval <= 0 {
		interval = defaultSchedulerInterval
	}
	if now == nil {
		now = time.Now
	}
	return &Scheduler{repo: repo, members: members, audit: audit, interval: interval, now: now}
}

// Run ticks at once and then every interval until ctx is canceled. A failed
// or panicking tick is logged and the next tick runs as usual.
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	s.safeTick(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.safeTick(ctx)
		}
	}
}

// safeTick runs one tick and keeps a panic from killing the process. The
// posting transaction is rolled back by GORM when it panics mid-way.
func (s *Scheduler) safeTick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.WriteLog(logger.LogLevelError, fmt.Sprintf("finance recurring scheduler panic: %v", r))
		}
	}()
	if err := s.Tick(ctx); err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("finance recurring scheduler: %v", err))
	}
}

// Tick posts the due occurrences of up to 100 templates, at most MaxCatchUp
// each, then writes the audit trail of what was committed.
func (s *Scheduler) Tick(ctx context.Context) error {
	if s == nil || s.repo == nil || s.members == nil {
		return errors.New("finance recurring scheduler is not configured")
	}
	now := s.now().UTC()
	b := &templateBuilder{ctx: ctx, members: s.members, now: now, cache: map[string][]domainspace.ResolvedMembership{}}
	result, err := s.repo.PostDueRecurring(ctx, now, schedulerBatch, MaxCatchUp, b.build)
	if err != nil {
		return fmt.Errorf("post due recurring: %w", err)
	}
	s.auditRun(ctx, b, result)
	if len(result.Posted)+len(result.Paused)+len(result.Skipped)+len(result.Dropped) > 0 {
		logger.WriteLog(logger.LogLevelInfo, fmt.Sprintf("finance recurring scheduler: posted=%d paused=%d skipped=%d templates_with_dropped=%d",
			len(result.Posted), len(result.Paused), len(result.Skipped), len(result.Dropped)))
	}
	return nil
}

// templateBuilder is the build callback of one tick. Before every occurrence
// it checks that the template's Space is still an active finance Space and its
// creator still an active member (a *PauseError otherwise); a failed lookup is
// returned as is, so the template is retried next tick, not paused. Members
// are read once per Space per tick.
type templateBuilder struct {
	ctx     context.Context
	members schedulerMembers
	now     time.Time
	cache   map[string][]domainspace.ResolvedMembership
}

func (b *templateBuilder) build(r domainfinance.Recurring, occurrence time.Time) (domainfinance.Transaction, error) {
	members, err := b.spaceMembers(r.SpaceID)
	if err != nil {
		return domainfinance.Transaction{}, err
	}
	if len(members) == 0 {
		return domainfinance.Transaction{}, domainfinance.Pause(domainfinance.PauseSpaceInvalid)
	}
	for _, m := range members {
		if strings.TrimSpace(m.SpaceCategory) != domainspace.CategoryFinance {
			return domainfinance.Transaction{}, domainfinance.Pause(domainfinance.PauseSpaceInvalid)
		}
	}
	if b.creator(r.SpaceID, r.CreatedByMemberID) == nil {
		return domainfinance.Transaction{}, domainfinance.Pause(domainfinance.PauseMemberInvalid)
	}
	return r.TransactionAt(occurrence, utils.CreateUUID(), b.now), nil
}

func (b *templateBuilder) spaceMembers(spaceID string) ([]domainspace.ResolvedMembership, error) {
	if members, ok := b.cache[spaceID]; ok {
		return members, nil
	}
	members, err := b.members.ListActiveMembers(b.ctx, spaceID)
	if err != nil {
		return nil, err
	}
	var active []domainspace.ResolvedMembership
	for _, m := range members {
		if strings.TrimSpace(m.SpaceID) == spaceID && strings.TrimSpace(m.Status) == domainspace.StatusActive {
			active = append(active, m)
		}
	}
	b.cache[spaceID] = active
	return active, nil
}

// creator returns the active member row of memberID, from the members this
// tick already read (nil when unknown or not read).
func (b *templateBuilder) creator(spaceID, memberID string) *domainspace.ResolvedMembership {
	for i, m := range b.cache[spaceID] {
		if strings.TrimSpace(m.ID) == memberID {
			return &b.cache[spaceID][i]
		}
	}
	return nil
}

// auditRun writes one event per posted transaction, per paused template and
// per template whose missed occurrences were dropped. It runs after the
// posting transaction committed and on a context that ignores cancellation,
// so a shutdown does not lose the trail. An audit failure is logged and
// never undoes or fails the posting: the money movement is already durable.
func (s *Scheduler) auditRun(ctx context.Context, b *templateBuilder, result domainfinance.PostResult) {
	if s.audit == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	for _, p := range result.Posted {
		event := s.schedulerEvent(b, p.SpaceID, p.MemberID, domainaudit.ActionCreate, resourceTransaction, p.TransactionID, domainaudit.StatusSuccess)
		event.Metadata["recurring_id"] = p.RecurringID
		s.store(ctx, event)
	}
	for _, issue := range result.Paused {
		event := s.schedulerEvent(b, issue.SpaceID, issue.MemberID, domainaudit.ActionUpdate, resourceRecurring, issue.RecurringID, domainaudit.StatusFailed)
		event.ErrorMessage = issue.Reason
		event.Message = "recurring template paused"
		event.Metadata["reason"], event.Metadata["paused"] = issue.Reason, true
		s.store(ctx, event)
	}
	for _, issue := range result.Skipped {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("finance recurring %s skipped this run: %s", issue.RecurringID, issue.Reason))
	}
	for recurringID, dropped := range result.Dropped {
		spaceID, memberID := templateOwner(result, recurringID)
		event := s.schedulerEvent(b, spaceID, memberID, domainaudit.ActionUpdate, resourceRecurring, recurringID, domainaudit.StatusSuccess)
		event.Message = "catch-up cap reached; missed occurrences dropped"
		event.Metadata["dropped"] = dropped
		s.store(ctx, event)
	}
}

// templateOwner finds the Space and creator of a template among the posted
// occurrences (a template only drops occurrences after posting the cap).
func templateOwner(result domainfinance.PostResult, recurringID string) (spaceID, memberID string) {
	for _, p := range result.Posted {
		if p.RecurringID == recurringID {
			return p.SpaceID, p.MemberID
		}
	}
	return "", ""
}

func (s *Scheduler) schedulerEvent(b *templateBuilder, spaceID, memberID, action, resource, resourceID, status string) domainaudit.AuditEvent {
	userID := ""
	if m := b.creator(spaceID, memberID); m != nil {
		userID = strings.TrimSpace(m.UserID)
	}
	return domainaudit.AuditEvent{
		ActorUserID: userID, ActorMemberID: auditID(memberID), Source: auditSourceScheduler,
		Action: action, Resource: resource, ResourceID: auditID(resourceID), Status: status,
		Metadata: map[string]any{"space_id": auditID(spaceID)},
	}
}

func (s *Scheduler) store(ctx context.Context, event domainaudit.AuditEvent) {
	if err := s.audit.Store(ctx, event); err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("finance recurring scheduler audit: %v", err))
	}
}
