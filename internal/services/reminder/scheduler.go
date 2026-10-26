package servicereminder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	interfacenotification "family-assistant/internal/interfaces/notification"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	"family-assistant/pkg/logger"
)

const (
	defaultSchedulerInterval        = 2 * time.Minute
	defaultSchedulerLease           = 2 * time.Minute
	defaultSchedulerBatch           = 50
	defaultDatabaseFallbackInterval = time.Minute
	defaultIndexReconcileInterval   = time.Hour
)

type ReminderSchedulerOptions struct {
	Interval                 time.Duration
	Lease                    time.Duration
	BatchSize                int
	DatabaseFallbackInterval time.Duration
	IndexReconcileInterval   time.Duration
	DueIndex                 interfacereminder.DueReminderIndex
	// Activities, when set, adds today's activity counts to keyed reminders.
	Activities reminderActivityCounter
}

type reminderActivityCounter interface {
	CountByKind(ctx context.Context, spaceID string, kinds []string, from, to time.Time) (map[string]int64, error)
}

type reminderIdentityResolver interface {
	FindActiveByUserID(context.Context, string, string) (*domainidentity.ExternalIdentity, error)
}

type reminderMembershipResolver interface {
	ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error)
}

type ReminderScheduler struct {
	reminders                interfacereminder.SchedulerRepository
	identities               reminderIdentityResolver
	members                  reminderMembershipResolver
	senders                  map[string]interfacenotification.NotificationSender
	index                    interfacereminder.DueReminderIndex
	interval                 time.Duration
	lease                    time.Duration
	batchSize                int
	databaseFallbackInterval time.Duration
	indexReconcileInterval   time.Duration
	activities               reminderActivityCounter
	lastDatabaseFallback     time.Time
	indexDegraded            bool
}

func NewReminderScheduler(
	reminders interfacereminder.SchedulerRepository,
	identities reminderIdentityResolver,
	members reminderMembershipResolver,
	senders map[string]interfacenotification.NotificationSender,
	options ReminderSchedulerOptions,
) *ReminderScheduler {
	if options.Interval <= 0 {
		options.Interval = defaultSchedulerInterval
	}
	if options.Lease <= 0 {
		options.Lease = defaultSchedulerLease
	}
	if options.BatchSize <= 0 {
		options.BatchSize = defaultSchedulerBatch
	}
	if options.DatabaseFallbackInterval <= 0 {
		options.DatabaseFallbackInterval = defaultDatabaseFallbackInterval
	}
	if options.IndexReconcileInterval <= 0 {
		options.IndexReconcileInterval = defaultIndexReconcileInterval
	}
	return &ReminderScheduler{
		reminders: reminders, identities: identities, members: members, senders: senders,
		index: options.DueIndex, interval: options.Interval, lease: options.Lease, batchSize: options.BatchSize,
		databaseFallbackInterval: options.DatabaseFallbackInterval, indexReconcileInterval: options.IndexReconcileInterval,
		activities: options.Activities,
	}
}

func (s *ReminderScheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	if err := s.reconcileReminderIndex(ctx); err != nil {
		s.indexDegraded = true
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("reminder scheduler index reconciliation: %v", err))
	} else {
		s.indexDegraded = false
	}
	if err := s.RunOnce(ctx, time.Now().UTC()); err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("reminder scheduler: %v", err))
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	reconcileTicker := time.NewTicker(s.indexReconcileInterval)
	defer reconcileTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.RunOnce(ctx, now.UTC()); err != nil {
				logger.WriteLog(logger.LogLevelError, fmt.Sprintf("reminder scheduler: %v", err))
			}
		case <-reconcileTicker.C:
			if err := s.reconcileReminderIndex(ctx); err != nil {
				s.indexDegraded = true
				logger.WriteLog(logger.LogLevelError, fmt.Sprintf("reminder scheduler index reconciliation: %v", err))
			} else {
				s.indexDegraded = false
			}
		}
	}
}

func (s *ReminderScheduler) RunOnce(ctx context.Context, now time.Time) error {
	if s == nil || s.reminders == nil {
		return errors.New("reminder scheduler is not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	hasDue, useDatabase, indexErr := s.checkDueSource(ctx, now)
	if !hasDue && !useDatabase {
		return nil
	}
	if indexErr != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("reminder scheduler using PostgreSQL fallback: %v", indexErr))
	}
	due, err := s.reminders.ClaimDueForNotification(ctx, now, now.Add(-s.lease), s.batchSize)
	if err != nil {
		return fmt.Errorf("claim due reminders: %w", err)
	}
	var failures []error
	for i := range due {
		release, deliveryErr := s.deliver(ctx, &due[i], now)
		if deliveryErr == nil {
			if s.index != nil {
				if err := s.index.Remove(ctx, due[i].ID); err != nil {
					failures = append(failures, fmt.Errorf("remove sent reminder %s from index: %w", due[i].ID, err))
				}
			}
			continue
		}
		failures = append(failures, fmt.Errorf("reminder %s: %w", due[i].ID, deliveryErr))
		if release {
			if due[i].NotificationClaimedAt == nil {
				failures = append(failures, fmt.Errorf("release reminder %s: claim timestamp is missing", due[i].ID))
				continue
			}
			if releaseErr := s.reminders.ReleaseNotificationClaim(ctx, due[i].ID, *due[i].NotificationClaimedAt); releaseErr != nil {
				if errors.Is(releaseErr, domainreminder.ErrStatusConflict) {
					logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("reminder changed before claim release: reminder %s", due[i].ID))
					continue
				}
				failures = append(failures, fmt.Errorf("release reminder %s: %w", due[i].ID, releaseErr))
			}
		}
	}
	return errors.Join(failures...)
}

func (s *ReminderScheduler) useDatabaseFallback(now time.Time) bool {
	if !s.lastDatabaseFallback.IsZero() && now.Sub(s.lastDatabaseFallback) < s.databaseFallbackInterval {
		return false
	}
	s.lastDatabaseFallback = now
	return true
}

func (s *ReminderScheduler) checkDueSource(ctx context.Context, now time.Time) (hasDue, useDatabase bool, indexErr error) {
	if s.index == nil {
		return false, s.useDatabaseFallback(now), nil
	}
	hasDue, err := s.index.HasDue(ctx, now)
	if err != nil {
		s.indexDegraded = true
	}
	if s.indexDegraded {
		if !s.useDatabaseFallback(now) {
			return false, false, err
		}
		if reconcileErr := s.reconcileReminderIndex(ctx); reconcileErr != nil {
			return false, true, errors.Join(err, reconcileErr)
		}
		hasDue, err = s.index.HasDue(ctx, now)
		if err != nil {
			s.indexDegraded = true
			return false, true, err
		}
		s.indexDegraded = false
	}
	s.lastDatabaseFallback = time.Time{}
	return hasDue, false, nil
}

func (s *ReminderScheduler) reconcileReminderIndex(ctx context.Context) error {
	if s == nil || s.index == nil || s.reminders == nil {
		return nil
	}
	indexedIDs, err := s.index.AllIDs(ctx)
	if err != nil {
		return fmt.Errorf("list indexed reminders: %w", err)
	}
	pending, err := s.reminders.ListPendingForNotification(ctx)
	if err != nil {
		return fmt.Errorf("list pending reminders: %w", err)
	}
	activeIDs := make(map[string]struct{}, len(pending))
	for i := range pending {
		activeIDs[pending[i].ID] = struct{}{}
	}
	if err := s.index.UpsertMany(ctx, pending); err != nil {
		return fmt.Errorf("upsert pending reminder index: %w", err)
	}
	staleIDs := make([]string, 0)
	for _, id := range indexedIDs {
		if _, ok := activeIDs[id]; !ok {
			staleIDs = append(staleIDs, id)
		}
	}
	if err := s.index.RemoveMany(ctx, staleIDs); err != nil {
		return fmt.Errorf("remove stale reminder index entries: %w", err)
	}
	return nil
}

func (s *ReminderScheduler) deliver(ctx context.Context, reminder *domainreminder.Reminder, sentAt time.Time) (bool, error) {
	target, err := s.target(ctx, reminder)
	if err != nil {
		return true, err
	}
	sender := s.senders[strings.ToLower(target.Provider)]
	if sender == nil {
		return true, fmt.Errorf("notification sender %q is not configured", target.Provider)
	}
	if err := sender.Send(ctx, target, reminderMessage(reminder, s.assigneeName(ctx, reminder), s.todaySummary(ctx, reminder, sentAt), sentAt)); err != nil {
		return true, err
	}
	if reminder.NotificationClaimedAt == nil {
		return true, errors.New("reminder claim timestamp is missing")
	}
	if err := s.reminders.MarkNotificationSent(ctx, reminder.ID, *reminder.NotificationClaimedAt, sentAt); err != nil {
		if errors.Is(err, domainreminder.ErrStatusConflict) {
			logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("reminder changed before delivery completed: reminder %s", reminder.ID))
			return false, nil
		}
		// Keep the claim until its lease expires: the message may already be delivered.
		return false, fmt.Errorf("mark notification sent: %w", err)
	}
	return false, nil
}

func (s *ReminderScheduler) target(ctx context.Context, reminder *domainreminder.Reminder) (domainreminder.DeliveryTarget, error) {
	provider := strings.ToLower(strings.TrimSpace(reminder.DeliveryProvider))
	target := strings.TrimSpace(reminder.DeliveryTarget)
	if provider != "" && target != "" {
		return domainreminder.DeliveryTarget{Provider: provider, Target: target}, nil
	}
	if provider != "" || target != "" {
		return domainreminder.DeliveryTarget{}, errors.New("incomplete reminder delivery target")
	}
	return s.fallbackTarget(ctx, reminder)
}

func (s *ReminderScheduler) fallbackTarget(ctx context.Context, reminder *domainreminder.Reminder) (domainreminder.DeliveryTarget, error) {
	if s.members == nil || s.identities == nil {
		return domainreminder.DeliveryTarget{}, errors.New("reminder delivery fallback is not configured")
	}
	members, err := s.members.ListActiveMembers(ctx, reminder.SpaceID)
	if err != nil {
		return domainreminder.DeliveryTarget{}, fmt.Errorf("list reminder members: %w", err)
	}
	for _, memberID := range reminderMemberIDs(reminder) {
		member := activeReminderMember(members, memberID)
		if target, ok := s.whatsappTarget(ctx, member); ok {
			return target, nil
		}
	}
	return domainreminder.DeliveryTarget{}, errors.New("no active WhatsApp delivery target")
}

func reminderMemberIDs(reminder *domainreminder.Reminder) []string {
	candidates := make([]string, 0, 2)
	if reminder.AssigneeMemberID != nil {
		candidates = append(candidates, *reminder.AssigneeMemberID)
	}
	candidates = append(candidates, reminder.CreatedByMemberID)

	memberIDs := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, memberID := range candidates {
		memberID = strings.TrimSpace(memberID)
		if memberID == "" {
			continue
		}
		if _, ok := seen[memberID]; ok {
			continue
		}
		seen[memberID] = struct{}{}
		memberIDs = append(memberIDs, memberID)
	}
	return memberIDs
}

func activeReminderMember(members []domainspace.ResolvedMembership, memberID string) *domainspace.ResolvedMembership {
	for i := range members {
		if strings.TrimSpace(members[i].ID) == memberID && members[i].Status == domainspace.StatusActive {
			return &members[i]
		}
	}
	return nil
}

func (s *ReminderScheduler) whatsappTarget(ctx context.Context, member *domainspace.ResolvedMembership) (domainreminder.DeliveryTarget, bool) {
	if member == nil || strings.TrimSpace(member.UserID) == "" {
		return domainreminder.DeliveryTarget{}, false
	}
	identity, err := s.identities.FindActiveByUserID(ctx, domainidentity.ProviderHermes, member.UserID)
	if err != nil || identity == nil || strings.TrimSpace(identity.ExternalID) == "" {
		return domainreminder.DeliveryTarget{}, false
	}
	return domainreminder.DeliveryTarget{Provider: "whatsapp", Target: strings.TrimSpace(identity.ExternalID)}, true
}

func (s *ReminderScheduler) assigneeName(ctx context.Context, reminder *domainreminder.Reminder) string {
	if s.members == nil || reminder.AssigneeMemberID == nil {
		return ""
	}
	members, err := s.members.ListActiveMembers(ctx, reminder.SpaceID)
	if err != nil {
		return ""
	}
	member := activeReminderMember(members, *reminder.AssigneeMemberID)
	if member == nil {
		return ""
	}
	return strings.TrimSpace(member.UserName)
}

// lateDeliveryThreshold is how late a reminder may be sent before the message
// shows its scheduled time.
const lateDeliveryThreshold = 10 * time.Minute

const defaultReplyFooter = "↩️ Reply pesan ini: *selesai*"

// summaryKinds lists the activity kinds counted for a reminder key; other keys
// count only their own kind.
var summaryKinds = map[string][]string{"diaper": {"diaper", "bowel_movement"}}

// summaryLabels are display names for activity kinds; unknown kinds show as is.
var summaryLabels = map[string]string{"diaper": "ganti popok", "bowel_movement": "poop", "feeding": "menyusu", "bath": "mandi"}

// todaySummary counts today's activities (since local midnight) of the kinds
// tied to a keyed reminder. It returns "" when there is nothing to show or the
// count fails, so a summary never blocks delivery.
func (s *ReminderScheduler) todaySummary(ctx context.Context, reminder *domainreminder.Reminder, sentAt time.Time) string {
	if s.activities == nil || reminder.ReplaceKey == nil || strings.TrimSpace(*reminder.ReplaceKey) == "" {
		return ""
	}
	key := strings.TrimSpace(*reminder.ReplaceKey)
	kinds := summaryKinds[key]
	if kinds == nil {
		kinds = []string{key}
	}
	local := sentAt.In(time.Local)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	counts, err := s.activities.CountByKind(ctx, reminder.SpaceID, kinds, midnight, sentAt)
	if err != nil {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("reminder summary skipped: reminder %s: %v", reminder.ID, err))
		return ""
	}
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		label := summaryLabels[kind]
		if label == "" {
			label = kind
		}
		parts = append(parts, fmt.Sprintf("%d× %s", counts[kind], label))
	}
	return "📊 Hari ini: " + strings.Join(parts, " · ")
}

func reminderMessage(reminder *domainreminder.Reminder, assigneeName, summary string, sentAt time.Time) string {
	lines := []string{reminderHeading(reminder.Title), ""}
	footer := defaultReplyFooter
	body := 0
	for _, line := range strings.Split(strings.TrimSpace(reminder.Description), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "↩️"):
			footer = line
		default:
			lines = append(lines, line)
			body++
		}
	}
	if summary = strings.TrimSpace(summary); summary != "" {
		lines = append(lines, summary)
		body++
	}
	if assigneeName := strings.TrimSpace(assigneeName); assigneeName != "" {
		lines = append(lines, "👤 Untuk: "+assigneeName)
		body++
	}
	if !reminder.ScheduledAt.IsZero() && sentAt.Sub(reminder.ScheduledAt) > lateDeliveryThreshold {
		scheduledAt := reminder.ScheduledAt.In(time.Local)
		deliveredAt := sentAt.In(time.Local)
		label := scheduledAt.Format("02/01 15.04 MST")
		if scheduledAt.Year() == deliveredAt.Year() && scheduledAt.YearDay() == deliveredAt.YearDay() {
			label = scheduledAt.Format("15.04 MST")
		}
		lines = append(lines, "⏱️ Terjadwal "+label+" · terkirim terlambat")
		body++
	}
	if body > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	return strings.Join(lines, "\n")
}

// reminderHeading bolds the title and keeps a leading emoji outside the bold
// span; a title without one gets a bell.
func reminderHeading(title string) string {
	title = strings.TrimSpace(title)
	first := []rune(title)
	if len(first) == 0 || unicode.IsLetter(first[0]) || unicode.IsDigit(first[0]) {
		return "🔔 *" + title + "*"
	}
	emoji, rest, found := strings.Cut(title, " ")
	if !found || strings.TrimSpace(rest) == "" {
		return "🔔 *" + title + "*"
	}
	return emoji + " *" + strings.TrimSpace(rest) + "*"
}
