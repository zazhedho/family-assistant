package servicereminder

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	interfacenotification "family-assistant/internal/interfaces/notification"
)

type schedulerRepositoryStub struct {
	due            []domainreminder.Reminder
	sent           []string
	sentClaims     []time.Time
	released       []string
	releasedClaims []time.Time
}

func (s *schedulerRepositoryStub) ClaimDueForNotification(_ context.Context, now, _ time.Time, _ int) ([]domainreminder.Reminder, error) {
	due := append([]domainreminder.Reminder(nil), s.due...)
	for i := range due {
		claimedAt := now
		due[i].NotificationClaimedAt = &claimedAt
	}
	return due, nil
}

func (s *schedulerRepositoryStub) MarkNotificationSent(_ context.Context, reminderID string, claimedAt, _ time.Time) error {
	s.sent = append(s.sent, reminderID)
	s.sentClaims = append(s.sentClaims, claimedAt)
	return nil
}

func (s *schedulerRepositoryStub) ReleaseNotificationClaim(_ context.Context, reminderID string, claimedAt time.Time) error {
	s.released = append(s.released, reminderID)
	s.releasedClaims = append(s.releasedClaims, claimedAt)
	return nil
}

type schedulerSenderStub struct {
	messages []sentNotification
	failFor  map[string]error
}

type sentNotification struct {
	target  domainreminder.DeliveryTarget
	message string
}

func (s *schedulerSenderStub) Send(_ context.Context, target domainreminder.DeliveryTarget, message string) error {
	if err := s.failFor[target.Target]; err != nil {
		return err
	}
	s.messages = append(s.messages, sentNotification{target: target, message: message})
	return nil
}

type membershipResolverStub struct {
	members map[string]string
}

func (s *membershipResolverStub) ListActiveMembers(_ context.Context, _ string) ([]domainspace.ResolvedMembership, error) {
	result := make([]domainspace.ResolvedMembership, 0, len(s.members))
	for memberID, userID := range s.members {
		if userID == "" {
			continue
		}
		result = append(result, domainspace.ResolvedMembership{ID: memberID, UserID: userID, Status: domainspace.StatusActive})
	}
	return result, nil
}

type identityResolverStub struct {
	identities map[string]*domainidentity.ExternalIdentity
}

func (s *identityResolverStub) FindActiveByUserID(_ context.Context, _, userID string) (*domainidentity.ExternalIdentity, error) {
	identity := s.identities[userID]
	if identity == nil {
		return nil, domainidentity.ErrIdentityNotFound
	}
	return identity, nil
}

func newScheduler(reminders *schedulerRepositoryStub, sender interfacenotification.NotificationSender, memberships *membershipResolverStub, identities *identityResolverStub) *ReminderScheduler {
	return NewReminderScheduler(reminders, identities, memberships, map[string]interfacenotification.NotificationSender{"whatsapp": sender}, time.Minute, time.Minute, 10)
}

func TestReminderSchedulerSendsStoredTextAndMarksNotification(t *testing.T) {
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", Title: "Susu bayi", Description: "Habiskan 120 ml", DeliveryProvider: "whatsapp", DeliveryTarget: "120363@g.us",
	}}}
	sender := &schedulerSenderStub{}
	scheduler := newScheduler(reminders, sender, &membershipResolverStub{}, &identityResolverStub{})

	if err := scheduler.RunOnce(context.Background(), time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0].target.Target != "120363@g.us" || sender.messages[0].message != "⏰ Pengingat: Susu bayi\nHabiskan 120 ml" {
		t.Fatalf("sent messages = %+v", sender.messages)
	}
	if len(reminders.sent) != 1 || reminders.sent[0] != "reminder-1" {
		t.Fatalf("marked sent = %v", reminders.sent)
	}
	if len(reminders.sentClaims) != 1 || !reminders.sentClaims[0].Equal(time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("sent claim = %v", reminders.sentClaims)
	}
}

func TestReminderSchedulerReleasesFailedSendAndContinues(t *testing.T) {
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{
		{ID: "reminder-1", Title: "First", DeliveryProvider: "whatsapp", DeliveryTarget: "chat-1"},
		{ID: "reminder-2", Title: "Second", DeliveryProvider: "whatsapp", DeliveryTarget: "chat-2"},
	}}
	sender := &schedulerSenderStub{failFor: map[string]error{"chat-1": errors.New("bridge down")}}
	scheduler := newScheduler(reminders, sender, &membershipResolverStub{}, &identityResolverStub{})

	if err := scheduler.RunOnce(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("expected aggregated delivery error")
	}
	if len(reminders.released) != 1 || reminders.released[0] != "reminder-1" {
		t.Fatalf("released = %v", reminders.released)
	}
	if len(reminders.releasedClaims) != 1 {
		t.Fatalf("released claims = %v", reminders.releasedClaims)
	}
	if len(reminders.sent) != 1 || reminders.sent[0] != "reminder-2" {
		t.Fatalf("marked sent = %v", reminders.sent)
	}
	if len(sender.messages) != 1 || sender.messages[0].target.Target != "chat-2" {
		t.Fatalf("messages = %+v", sender.messages)
	}
}

func TestReminderSchedulerFallsBackToAssigneeIdentity(t *testing.T) {
	assignee := "member-assignee"
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", SpaceID: "space-1", CreatedByMemberID: "member-creator", AssigneeMemberID: &assignee, Title: "Check baby",
	}}}
	sender := &schedulerSenderStub{}
	memberships := &membershipResolverStub{members: map[string]string{"member-assignee": "user-assignee"}}
	identities := &identityResolverStub{identities: map[string]*domainidentity.ExternalIdentity{
		"user-assignee": {Provider: domainidentity.ProviderHermes, ExternalID: "6285333320090@s.whatsapp.net", Status: domainidentity.StatusActive},
	}}
	scheduler := newScheduler(reminders, sender, memberships, identities)

	if err := scheduler.RunOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0].target != (domainreminder.DeliveryTarget{Provider: "whatsapp", Target: "6285333320090@s.whatsapp.net"}) {
		t.Fatalf("fallback target = %+v", sender.messages)
	}
}

func TestReminderSchedulerFallsBackToCreatorIdentity(t *testing.T) {
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", SpaceID: "space-1", CreatedByMemberID: "member-creator", Title: "Check baby",
	}}}
	sender := &schedulerSenderStub{}
	memberships := &membershipResolverStub{members: map[string]string{"member-creator": "user-creator"}}
	identities := &identityResolverStub{identities: map[string]*domainidentity.ExternalIdentity{
		"user-creator": {Provider: domainidentity.ProviderHermes, ExternalID: "creator@s.whatsapp.net", Status: domainidentity.StatusActive},
	}}
	scheduler := newScheduler(reminders, sender, memberships, identities)

	if err := scheduler.RunOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0].target.Target != "creator@s.whatsapp.net" {
		t.Fatalf("fallback target = %+v", sender.messages)
	}
}
