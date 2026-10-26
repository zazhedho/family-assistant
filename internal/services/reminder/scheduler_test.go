package servicereminder

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	interfacenotification "family-assistant/internal/interfaces/notification"
)

type schedulerRepositoryStub struct {
	due              []domainreminder.Reminder
	pending          []domainreminder.Reminder
	pendingErr       error
	pendingListCalls chan struct{}
	claimCalls       int
	sent             []string
	sentClaims       []time.Time
	released         []string
	releasedClaims   []time.Time
	markSentErr      error
	releaseErr       error
}

func (s *schedulerRepositoryStub) ListPendingForNotification(context.Context) ([]domainreminder.Reminder, error) {
	if s.pendingListCalls != nil {
		select {
		case s.pendingListCalls <- struct{}{}:
		default:
		}
	}
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
	return s.markSentErr
}

func (s *schedulerRepositoryStub) ReleaseNotificationClaim(_ context.Context, reminderID string, claimedAt time.Time) error {
	s.released = append(s.released, reminderID)
	s.releasedClaims = append(s.releasedClaims, claimedAt)
	return s.releaseErr
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

func TestReminderSchedulerUsesConfiguredPostgresFallbackIntervalOnRedisFailure(t *testing.T) {
	start := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{}
	index := &dueReminderIndexStub{hasDueErr: errors.New("redis unavailable")}
	scheduler := NewReminderScheduler(reminders, nil, nil, nil, ReminderSchedulerOptions{
		DueIndex: index, DatabaseFallbackInterval: 2 * time.Minute,
	})

	for _, now := range []time.Time{start, start.Add(time.Minute), start.Add(2 * time.Minute)} {
		if err := scheduler.RunOnce(context.Background(), now); err != nil {
			t.Fatalf("run once at %s: %v", now, err)
		}
	}
	if reminders.claimCalls != 2 {
		t.Fatalf("PostgreSQL fallback claims = %d, want 2", reminders.claimCalls)
	}
}

func TestReminderSchedulerUsesConfiguredIndexReconcileInterval(t *testing.T) {
	reminders := &schedulerRepositoryStub{pendingListCalls: make(chan struct{}, 2)}
	scheduler := NewReminderScheduler(reminders, nil, nil, nil, ReminderSchedulerOptions{
		Interval: time.Hour, DatabaseFallbackInterval: time.Minute,
		IndexReconcileInterval: 10 * time.Millisecond, DueIndex: &dueReminderIndexStub{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-reminders.pendingListCalls:
		case <-time.After(time.Second):
			cancel()
			<-done
			t.Fatal("expected startup and periodic reconciliation")
		}
	}
	cancel()
	<-done
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
	wantMessage := "🔔 *Susu bayi*\n\nHabiskan 120 ml\n👤 Untuk: Mommy Zeia\n\n↩️ Reply pesan ini: *selesai*"
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

func TestDeliverIgnoresStatusConflictAfterSend(t *testing.T) {
	reminders := &schedulerRepositoryStub{
		due:         []domainreminder.Reminder{{ID: "reminder-1", Title: "Replaced", DeliveryProvider: "whatsapp", DeliveryTarget: "chat-1"}},
		markSentErr: domainreminder.ErrStatusConflict,
	}
	sender := &schedulerSenderStub{}
	scheduler := newScheduler(reminders, sender, &membershipResolverStub{}, &identityResolverStub{})

	if err := scheduler.RunOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if len(reminders.sent) != 1 || len(sender.messages) != 1 {
		t.Fatalf("sent = %v, messages = %+v", reminders.sent, sender.messages)
	}
	if len(reminders.released) != 0 {
		t.Fatalf("released = %v, want none", reminders.released)
	}
	if reminders.claimCalls != 1 {
		t.Fatalf("claim calls = %d, want 1", reminders.claimCalls)
	}
}

func TestReleaseIgnoresStatusConflict(t *testing.T) {
	reminders := &schedulerRepositoryStub{
		due:        []domainreminder.Reminder{{ID: "reminder-1", Title: "Replaced", DeliveryProvider: "whatsapp", DeliveryTarget: "chat-1"}},
		releaseErr: domainreminder.ErrStatusConflict,
	}
	sender := &schedulerSenderStub{failFor: map[string]error{"chat-1": errors.New("bridge down")}}
	scheduler := newScheduler(reminders, sender, &membershipResolverStub{}, &identityResolverStub{})

	err := scheduler.RunOnce(context.Background(), time.Now().UTC())
	if err == nil || errors.Is(err, domainreminder.ErrStatusConflict) {
		t.Fatalf("err = %v, want delivery error without status conflict", err)
	}
	if len(reminders.released) != 1 {
		t.Fatalf("released = %v, want exactly one attempt", reminders.released)
	}
	if reminders.claimCalls != 1 {
		t.Fatalf("claim calls = %d, want 1", reminders.claimCalls)
	}
}

type activityCounterStub struct {
	counts   map[string]int64
	err      error
	calls    int
	spaceID  string
	kinds    []string
	from, to time.Time
}

func (s *activityCounterStub) CountByKind(_ context.Context, spaceID string, kinds []string, from, to time.Time) (map[string]int64, error) {
	s.calls++
	s.spaceID, s.kinds, s.from, s.to = spaceID, kinds, from, to
	return s.counts, s.err
}

func TestReminderMessageLayout(t *testing.T) {
	scheduled := time.Date(2026, 10, 26, 21, 4, 0, 0, time.Local)
	tests := []struct {
		name     string
		reminder domainreminder.Reminder
		assignee string
		summary  string
		sentAt   time.Time
		want     string
	}{
		{
			name: "emoji title keeps its emoji and moves the reply hint to the footer",
			reminder: domainreminder.Reminder{
				Title:       "🧷 Waktunya cek popok Baby Zeia",
				Description: "Terakhir diganti *17.04* oleh Mommy Zeia · 4 jam lalu.\n↩️ Reply pesan ini: *selesai* (atau \"selesai, poop\")",
				ScheduledAt: scheduled,
			},
			summary: "📊 Hari ini: 6× ganti popok · 2× poop",
			sentAt:  scheduled,
			want:    "🧷 *Waktunya cek popok Baby Zeia*\n\nTerakhir diganti *17.04* oleh Mommy Zeia · 4 jam lalu.\n📊 Hari ini: 6× ganti popok · 2× poop\n\n↩️ Reply pesan ini: *selesai* (atau \"selesai, poop\")",
		},
		{
			name:     "plain title gets a bell and the default footer",
			reminder: domainreminder.Reminder{Title: "Bayar listrik", ScheduledAt: scheduled},
			sentAt:   scheduled.Add(9 * time.Minute),
			want:     "🔔 *Bayar listrik*\n\n↩️ Reply pesan ini: *selesai*",
		},
		{
			name:     "late delivery on the same day shows the scheduled time",
			reminder: domainreminder.Reminder{Title: "Bayar listrik", ScheduledAt: scheduled},
			assignee: "Daddy Zeia",
			sentAt:   scheduled.Add(25 * time.Minute),
			want:     "🔔 *Bayar listrik*\n\n👤 Untuk: Daddy Zeia\n⏱️ Terjadwal " + scheduled.Format("15.04 MST") + " · terkirim terlambat\n\n↩️ Reply pesan ini: *selesai*",
		},
		{
			name:     "late delivery on a later day shows the date",
			reminder: domainreminder.Reminder{Title: "Bayar listrik", ScheduledAt: scheduled},
			sentAt:   scheduled.Add(5 * time.Hour),
			want:     "🔔 *Bayar listrik*\n\n⏱️ Terjadwal " + scheduled.Format("02/01 15.04 MST") + " · terkirim terlambat\n\n↩️ Reply pesan ini: *selesai*",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reminderMessage(&tt.reminder, tt.assignee, tt.summary, tt.sentAt); got != tt.want {
				t.Fatalf("message mismatch\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestReminderSchedulerAddsTodaySummaryForKeyedReminder(t *testing.T) {
	key := "diaper"
	runAt := time.Date(2026, 10, 26, 14, 4, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", SpaceID: "space-1", Title: "🧷 Waktunya cek popok Baby Zeia", ScheduledAt: runAt,
		ReplaceKey: &key, DeliveryProvider: "whatsapp", DeliveryTarget: "120363@g.us",
	}}}
	sender := &schedulerSenderStub{}
	counter := &activityCounterStub{counts: map[string]int64{"diaper": 6}}
	scheduler := NewReminderScheduler(reminders, &identityResolverStub{}, &membershipResolverStub{}, map[string]interfacenotification.NotificationSender{"whatsapp": sender},
		ReminderSchedulerOptions{Interval: time.Minute, Lease: time.Minute, BatchSize: 10, Activities: counter})

	if err := scheduler.RunOnce(context.Background(), runAt); err != nil {
		t.Fatalf("run once: %v", err)
	}
	local := runAt.In(time.Local)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	if counter.spaceID != "space-1" || !reflect.DeepEqual(counter.kinds, []string{"diaper", "bowel_movement"}) || !counter.from.Equal(midnight) || !counter.to.Equal(runAt) {
		t.Fatalf("count call = space %q kinds %v from %v to %v", counter.spaceID, counter.kinds, counter.from, counter.to)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].message, "\n📊 Hari ini: 6× ganti popok · 0× poop\n") {
		t.Fatalf("sent messages = %+v", sender.messages)
	}
}

func TestReminderSchedulerSendsWithoutSummaryWhenCountFails(t *testing.T) {
	key := "bath"
	runAt := time.Date(2026, 10, 26, 14, 4, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", SpaceID: "space-1", Title: "Mandi", ScheduledAt: runAt, ReplaceKey: &key,
		DeliveryProvider: "whatsapp", DeliveryTarget: "120363@g.us",
	}}}
	sender := &schedulerSenderStub{}
	counter := &activityCounterStub{err: errors.New("db down")}
	scheduler := NewReminderScheduler(reminders, &identityResolverStub{}, &membershipResolverStub{}, map[string]interfacenotification.NotificationSender{"whatsapp": sender},
		ReminderSchedulerOptions{Interval: time.Minute, Lease: time.Minute, BatchSize: 10, Activities: counter})

	if err := scheduler.RunOnce(context.Background(), runAt); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !reflect.DeepEqual(counter.kinds, []string{"bath"}) {
		t.Fatalf("kinds = %v, want the key itself", counter.kinds)
	}
	if len(sender.messages) != 1 || strings.Contains(sender.messages[0].message, "📊") || len(reminders.sent) != 1 {
		t.Fatalf("sent messages = %+v, marked sent = %v", sender.messages, reminders.sent)
	}
}

func TestReminderSchedulerSkipsSummaryWithoutKey(t *testing.T) {
	runAt := time.Date(2026, 10, 26, 14, 4, 0, 0, time.UTC)
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", SpaceID: "space-1", Title: "Sisa susu", ScheduledAt: runAt, DeliveryProvider: "whatsapp", DeliveryTarget: "120363@g.us",
	}}}
	sender := &schedulerSenderStub{}
	counter := &activityCounterStub{}
	scheduler := NewReminderScheduler(reminders, &identityResolverStub{}, &membershipResolverStub{}, map[string]interfacenotification.NotificationSender{"whatsapp": sender},
		ReminderSchedulerOptions{Interval: time.Minute, Lease: time.Minute, BatchSize: 10, Activities: counter})

	if err := scheduler.RunOnce(context.Background(), runAt); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if counter.calls != 0 || len(sender.messages) != 1 || strings.Contains(sender.messages[0].message, "📊") {
		t.Fatalf("calls = %d, messages = %+v", counter.calls, sender.messages)
	}
}

func TestReminderHeadingEdgeCases(t *testing.T) {
	tests := map[string]string{
		"🧷 Waktunya cek popok":    "🧷 *Waktunya cek popok*",
		"🧷Waktunya cek popok":     "🧷 *Waktunya cek popok*",
		"👩‍🍼 Susu malam":          "👩‍🍼 *Susu malam*",
		"🛁️ Mandi sore":           "🛁️ *Mandi sore*",
		"(Penting) Bayar listrik": "🔔 *(Penting) Bayar listrik*",
		"*Penting* bayar":         "🔔 *Penting bayar*",
		"🧷":                       "🔔 *🧷*",
		"Bayar listrik":           "🔔 *Bayar listrik*",
		"***":                     "🔔 *Pengingat*",
	}
	for title, want := range tests {
		if got := reminderHeading(title); got != want {
			t.Errorf("reminderHeading(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestReminderMessageSummaryWithoutDescriptionAndCRLF(t *testing.T) {
	scheduled := time.Date(2026, 10, 26, 21, 4, 0, 0, time.Local)
	got := reminderMessage(&domainreminder.Reminder{Title: "🛁 Mandi", ScheduledAt: scheduled}, "", "📊 Hari ini: 1× mandi", scheduled)
	if want := "🛁 *Mandi*\n\n📊 Hari ini: 1× mandi\n\n↩️ Reply pesan ini: *selesai*"; got != want {
		t.Fatalf("summary only:\n got: %q\nwant: %q", got, want)
	}
	got = reminderMessage(&domainreminder.Reminder{Title: "🍼 Sisa susu", Description: "Dari feeding 14.29.\r\n↩ Reply: *habis*\r\n", ScheduledAt: scheduled}, "", "", scheduled)
	if want := "🍼 *Sisa susu*\n\nDari feeding 14.29.\n\n↩ Reply: *habis*"; got != want {
		t.Fatalf("crlf and bare arrow:\n got: %q\nwant: %q", got, want)
	}
}

func TestReminderSchedulerSummaryStartsAtJakartaMidnight(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	previous := time.Local
	time.Local = jakarta
	t.Cleanup(func() { time.Local = previous })

	key := "diaper"
	runAt := time.Date(2026, 10, 25, 17, 30, 0, 0, time.UTC) // 00:30 WIB on 26 Oct
	reminders := &schedulerRepositoryStub{due: []domainreminder.Reminder{{
		ID: "reminder-1", SpaceID: "space-1", Title: "🧷 Cek popok", ScheduledAt: runAt, ReplaceKey: &key,
		DeliveryProvider: "whatsapp", DeliveryTarget: "120363@g.us",
	}}}
	counter := &activityCounterStub{}
	scheduler := NewReminderScheduler(reminders, &identityResolverStub{}, &membershipResolverStub{}, map[string]interfacenotification.NotificationSender{"whatsapp": &schedulerSenderStub{}},
		ReminderSchedulerOptions{Interval: time.Minute, Lease: time.Minute, BatchSize: 10, Activities: counter})

	if err := scheduler.RunOnce(context.Background(), runAt); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if want := time.Date(2026, 10, 25, 17, 0, 0, 0, time.UTC); !counter.from.Equal(want) {
		t.Fatalf("summary from = %v, want %v (00:00 WIB on 26 Oct)", counter.from.UTC(), want)
	}
}
