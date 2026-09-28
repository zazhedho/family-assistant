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
	pending        []domainreminder.Reminder
	pendingErr     error
	claimCalls     int
	sent           []string
	sentClaims     []time.Time
	released       []string
	releasedClaims []time.Time
}

func (s *schedulerRepositoryStub) ListPendingForNotification(context.Context) ([]domainreminder.Reminder, error) {
	return append([]domainreminder.Reminder(nil), s.pending...), s.pendingErr
}

func (s *schedulerRepositoryStub) ClaimDueForNotification(_ context.Context, now, _ time.Time, _ int) ([]domainreminder.Reminder, error) {
	s.claimCalls++
	due := append([]domainreminder.Reminder(nil), s.due...)
	for i := range due {
		claimedAt := now
		due[i].NotificationClaimedAt = &claimedAt
	}
	return due, nil
}

type dueReminderIndexStub struct {
	hasDue      bool
	hasDueErr   error
	ids         []string
	upserts     []reminderIndexWrite
	bulkUpserts []domainreminder.Reminder
	removed     []string
	bulkRemoved []string
}

func (s *dueReminderIndexStub) Upsert(_ context.Context, id string, scheduledAt time.Time) error {
	s.upserts = append(s.upserts, reminderIndexWrite{id: id, scheduledAt: scheduledAt})
	return nil
}

func (s *dueReminderIndexStub) Remove(_ context.Context, id string) error {
	s.removed = append(s.removed, id)
	return nil
}

func (s *dueReminderIndexStub) HasDue(context.Context, time.Time) (bool, error) {
	return s.hasDue, s.hasDueErr
}

func (s *dueReminderIndexStub) AllIDs(context.Context) ([]string, error) {
	return append([]string(nil), s.ids...), nil
}

func (s *dueReminderIndexStub) UpsertMany(_ context.Context, reminders []domainreminder.Reminder) error {
	s.bulkUpserts = append([]domainreminder.Reminder(nil), reminders...)
	return nil
}

func (s *dueReminderIndexStub) RemoveMany(_ context.Context, ids []string) error {
	s.bulkRemoved = append([]string(nil), ids...)
	return nil
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
	names   map[string]string
}

func (s *membershipResolverStub) ListActiveMembers(_ context.Context, _ string) ([]domainspace.ResolvedMembership, error) {
	result := make([]domainspace.ResolvedMembership, 0, len(s.members))
	for memberID, userID := range s.members {
		if userID == "" {
			continue
		}
		result = append(result, domainspace.ResolvedMembership{ID: memberID, UserID: userID, UserName: s.names[memberID], Status: domainspace.StatusActive})
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
	return NewReminderScheduler(reminders, identities, memberships, map[string]interfacenotification.NotificationSender{"whatsapp": sender}, ReminderSchedulerOptions{Interval: time.Minute, Lease: time.Minute, BatchSize: 10})
}

func TestReminderSchedulerSkipsPostgresWhenRedisHasNoDueReminder(t *testing.T) {
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{ID: "reminder-1"}}}
	index := &dueReminderIndexStub{}
	scheduler := NewReminderScheduler(reminders, nil, nil, nil, ReminderSchedulerOptions{DueIndex: index})

	if err := scheduler.RunOnce(context.Background(), time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if reminders.claimCalls != 0 {
		t.Fatalf("PostgreSQL claims = %d, want 0", reminders.claimCalls)
	}
}

func TestReminderSchedulerClaimsFromPostgresWhenRedisHasDueReminder(t *testing.T) {
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{ID: "reminder-1", Title: "Feed", DeliveryProvider: "whatsapp", DeliveryTarget: "chat-1"}}}
	index := &dueReminderIndexStub{hasDue: true}
	sender := &schedulerSenderStub{}
	scheduler := NewReminderScheduler(reminders, nil, nil, map[string]interfacenotification.NotificationSender{"whatsapp": sender}, ReminderSchedulerOptions{DueIndex: index})

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if reminders.claimCalls != 1 || len(sender.messages) != 1 {
		t.Fatalf("claims=%d sent=%d, want one each", reminders.claimCalls, len(sender.messages))
	}
	if len(index.removed) != 1 || index.removed[0] != "reminder-1" {
		t.Fatalf("removed index IDs = %v", index.removed)
	}
}

func TestReminderSchedulerFallsBackToPostgresOncePerMinuteOnRedisFailure(t *testing.T) {
	start := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{}
	index := &dueReminderIndexStub{hasDueErr: errors.New("redis unavailable")}
	scheduler := NewReminderScheduler(reminders, nil, nil, nil, ReminderSchedulerOptions{DueIndex: index})

	for _, now := range []time.Time{start, start.Add(30 * time.Second), start.Add(time.Minute)} {
		if err := scheduler.RunOnce(context.Background(), now); err != nil {
			t.Fatalf("run once at %s: %v", now, err)
		}
	}
	if reminders.claimCalls != 2 {
		t.Fatalf("PostgreSQL fallback claims = %d, want 2", reminders.claimCalls)
	}
}

func TestReminderSchedulerReconcilesIndexAfterRedisRecovery(t *testing.T) {
	start := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{pending: []domainreminder.Reminder{{ID: "missed-during-outage"}}}
	index := &dueReminderIndexStub{ids: []string{"stale-reminder"}, hasDueErr: errors.New("redis unavailable")}
	scheduler := NewReminderScheduler(reminders, nil, nil, nil, ReminderSchedulerOptions{DueIndex: index})

	if err := scheduler.RunOnce(context.Background(), start); err != nil {
		t.Fatalf("run during Redis outage: %v", err)
	}
	index.hasDueErr = nil
	if err := scheduler.RunOnce(context.Background(), start.Add(time.Minute)); err != nil {
		t.Fatalf("run after Redis recovery: %v", err)
	}
	if len(index.bulkUpserts) != 1 || index.bulkUpserts[0].ID != "missed-during-outage" {
		t.Fatalf("reconciled reminders after recovery = %+v", index.bulkUpserts)
	}
}

func TestReminderSchedulerReconcilesRedisIndexAndRemovesStaleIDs(t *testing.T) {
	reminders := &schedulerRepositoryStub{
		pending: []domainreminder.Reminder{
			{ID: "reminder-1"},
			{ID: "reminder-2"},
		},
	}
	index := &dueReminderIndexStub{ids: []string{"reminder-1", "stale-reminder"}}
	scheduler := NewReminderScheduler(reminders, nil, nil, nil, ReminderSchedulerOptions{DueIndex: index})

	if err := scheduler.reconcileReminderIndex(context.Background()); err != nil {
		t.Fatalf("reconcile reminder index: %v", err)
	}
	if len(index.bulkUpserts) != 2 || index.bulkUpserts[0].ID != "reminder-1" || index.bulkUpserts[1].ID != "reminder-2" {
		t.Fatalf("reconciled reminders = %+v", index.bulkUpserts)
	}
	if len(index.bulkRemoved) != 1 || index.bulkRemoved[0] != "stale-reminder" {
		t.Fatalf("stale IDs removed = %v", index.bulkRemoved)
	}
}

func TestReminderSchedulerFormatsNotificationAndMarksSent(t *testing.T) {
	assigneeID := "member-1"
	runAt := time.Date(2026, 9, 25, 12, 27, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", Title: "Susu bayi", Description: "Habiskan 120 ml", ScheduledAt: time.Date(2026, 9, 25, 12, 27, 0, 0, time.UTC),
		AssigneeMemberID: &assigneeID, DeliveryProvider: "whatsapp", DeliveryTarget: "120363@g.us",
	}}}
	sender := &schedulerSenderStub{}
	memberships := &membershipResolverStub{members: map[string]string{assigneeID: "user-1"}, names: map[string]string{assigneeID: "Mommy Zeia"}}
	scheduler := newScheduler(reminders, sender, memberships, &identityResolverStub{})

	if err := scheduler.RunOnce(context.Background(), runAt); err != nil {
		t.Fatalf("run once: %v", err)
	}
	localScheduledAt := reminders.due[0].ScheduledAt.In(time.Local)
	wantMessage := "🔔 *!!! REMINDER !!!*\n\n*Susu bayi*\nHabiskan 120 ml\n\n🕒 Hari ini · " + localScheduledAt.Format("15.04 MST") + "\n👤 Untuk: Mommy Zeia\n\nBalas *selesai* jika sudah dilakukan."
	if len(sender.messages) != 1 || sender.messages[0].target.Target != "120363@g.us" || sender.messages[0].message != wantMessage {
		t.Fatalf("sent messages = %+v", sender.messages)
	}
	if len(reminders.sent) != 1 || reminders.sent[0] != "reminder-1" {
		t.Fatalf("marked sent = %v", reminders.sent)
	}
	if len(reminders.sentClaims) != 1 || !reminders.sentClaims[0].Equal(runAt) {
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
