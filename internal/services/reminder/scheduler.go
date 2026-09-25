package servicereminder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	interfacenotification "family-assistant/internal/interfaces/notification"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	"family-assistant/pkg/logger"
)

const (
	defaultSchedulerInterval = 30 * time.Second
	defaultSchedulerLease    = 2 * time.Minute
	defaultSchedulerBatch    = 50
)

type reminderIdentityResolver interface {
	FindActiveByUserID(context.Context, string, string) (*domainidentity.ExternalIdentity, error)
}

type reminderMembershipResolver interface {
	ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error)
}

type ReminderScheduler struct {
	reminders  interfacereminder.SchedulerRepository
	identities reminderIdentityResolver
	members    reminderMembershipResolver
	senders    map[string]interfacenotification.NotificationSender
	interval   time.Duration
	lease      time.Duration
	batchSize  int
}

func NewReminderScheduler(
	reminders interfacereminder.SchedulerRepository,
	identities reminderIdentityResolver,
	members reminderMembershipResolver,
	senders map[string]interfacenotification.NotificationSender,
	interval, lease time.Duration,
	batchSize int,
) *ReminderScheduler {
	if interval <= 0 {
		interval = defaultSchedulerInterval
	}
	if lease <= 0 {
		lease = defaultSchedulerLease
	}
	if batchSize <= 0 {
		batchSize = defaultSchedulerBatch
	}
	return &ReminderScheduler{
		reminders: reminders, identities: identities, members: members, senders: senders,
		interval: interval, lease: lease, batchSize: batchSize,
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
	if err := s.RunOnce(ctx, time.Now().UTC()); err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("reminder scheduler: %v", err))
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.RunOnce(ctx, now.UTC()); err != nil {
				logger.WriteLog(logger.LogLevelError, fmt.Sprintf("reminder scheduler: %v", err))
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
	due, err := s.reminders.ClaimDueForNotification(ctx, now, now.Add(-s.lease), s.batchSize)
	if err != nil {
		return fmt.Errorf("claim due reminders: %w", err)
	}
	var failures []error
	for i := range due {
		release, deliveryErr := s.deliver(ctx, &due[i], now)
		if deliveryErr == nil {
			continue
		}
		failures = append(failures, fmt.Errorf("reminder %s: %w", due[i].ID, deliveryErr))
		if release {
			if due[i].NotificationClaimedAt == nil {
				failures = append(failures, fmt.Errorf("release reminder %s: claim timestamp is missing", due[i].ID))
				continue
			}
			if releaseErr := s.reminders.ReleaseNotificationClaim(ctx, due[i].ID, *due[i].NotificationClaimedAt); releaseErr != nil {
				failures = append(failures, fmt.Errorf("release reminder %s: %w", due[i].ID, releaseErr))
			}
		}
	}
	return errors.Join(failures...)
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
	if err := sender.Send(ctx, target, reminderMessage(reminder)); err != nil {
		return true, err
	}
	if reminder.NotificationClaimedAt == nil {
		return true, errors.New("reminder claim timestamp is missing")
	}
	if err := s.reminders.MarkNotificationSent(ctx, reminder.ID, *reminder.NotificationClaimedAt, sentAt); err != nil {
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

func reminderMessage(reminder *domainreminder.Reminder) string {
	message := "⏰ Pengingat: " + strings.TrimSpace(reminder.Title)
	if description := strings.TrimSpace(reminder.Description); description != "" {
		message += "\n" + description
	}
	return message
}
