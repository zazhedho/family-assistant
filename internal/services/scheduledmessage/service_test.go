package servicescheduledmessage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceaudit "family-assistant/internal/interfaces/audit"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	interfacespace "family-assistant/internal/interfaces/space"
	"family-assistant/internal/services/authorization"
	"family-assistant/pkg/filter"
	"family-assistant/pkg/logger"
	"gorm.io/gorm"
)

const (
	serviceSpaceID         = "00000000-0000-4000-8000-000000000201"
	otherServiceSpaceID    = "00000000-0000-4000-8000-000000000202"
	serviceCreatorID       = "00000000-0000-4000-8000-000000000301"
	serviceRecipientID     = "00000000-0000-4000-8000-000000000302"
	otherRecipientID       = "00000000-0000-4000-8000-000000000303"
	serviceScheduleID      = "00000000-0000-4000-8000-000000000101"
	serviceDeliveryID      = "00000000-0000-4000-8000-000000000401"
	serviceOtherDeliveryID = "00000000-0000-4000-8000-000000000402"
	serviceChannelID       = "00000000-0000-4000-8000-000000000501"
)

type scheduledMessageRepoStub struct {
	pendingMu                   sync.Mutex
	pendingForIndex             []domainscheduledmessage.Delivery
	afterMutation               func()
	created                     *domainscheduledmessage.Schedule
	firstDelivery               *domainscheduledmessage.Delivery
	found                       *domainscheduledmessage.Schedule
	listed                      []domainscheduledmessage.Schedule
	listInput                   dto.ScheduledMessageListInput
	history                     []domainscheduledmessage.Delivery
	historyLimit                int
	historySpaceID              string
	historyScheduleID           string
	updatedFields               dto.ScheduledMessageUpdateInput
	updatedDelivery             *domainscheduledmessage.Delivery
	updatedSpaceID              string
	updatedScheduleID           string
	updatedCancelledDeliveryIDs []string
	cancelledDeliveryIDs        []string
	unsubscribedDeliveryIDs     []string
	updatedAt                   time.Time
	updateCalls                 int
	cancelCalls                 int
	unsubscribeCalls            int
	bound                       *domainscheduledmessage.SpaceChannel
	bindCalls                   int
	bindSpaceID                 string
	bindMemberID                string
	bindChannel                 string
	bindChatID                  string
	bindChatType                string
	targets                     []dto.ScheduledMessageTargetOutput
	targetCalls                 int
}

func (s *scheduledMessageRepoStub) Create(_ context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery) error {
	if schedule.ID == "" {
		schedule.ID = serviceScheduleID
	}
	if delivery.ID == "" {
		delivery.ID = serviceDeliveryID
	}
	scheduleCopy, deliveryCopy := *schedule, *delivery
	s.created, s.firstDelivery = &scheduleCopy, &deliveryCopy
	s.setPendingForIndex([]domainscheduledmessage.Delivery{deliveryCopy})
	s.afterPendingMutation()
	return nil
}

func (s *scheduledMessageRepoStub) setPendingForIndex(rows []domainscheduledmessage.Delivery) {
	s.pendingMu.Lock()
	s.pendingForIndex = append([]domainscheduledmessage.Delivery(nil), rows...)
	s.pendingMu.Unlock()
}

func (s *scheduledMessageRepoStub) afterPendingMutation() {
	if s.afterMutation != nil {
		s.afterMutation()
	}
}

func (s *scheduledMessageRepoStub) FindByIDInSpace(_ context.Context, spaceID, scheduleID string) (*domainscheduledmessage.Schedule, error) {
	if s.found == nil || s.found.SpaceID != spaceID || s.found.ID != scheduleID {
		return nil, gorm.ErrRecordNotFound
	}
	cloned := *s.found
	return &cloned, nil
}

func (s *scheduledMessageRepoStub) List(_ context.Context, input dto.ScheduledMessageListInput) ([]domainscheduledmessage.Schedule, error) {
	s.listInput = input
	result := make([]domainscheduledmessage.Schedule, 0, len(s.listed))
	for _, schedule := range s.listed {
		if input.ScheduleID != nil && schedule.ID != *input.ScheduleID {
			continue
		}
		result = append(result, schedule)
	}
	return result, nil
}

func (s *scheduledMessageRepoStub) ListHistory(_ context.Context, spaceID, scheduleID string, limit int) ([]domainscheduledmessage.Delivery, error) {
	s.historySpaceID, s.historyScheduleID, s.historyLimit = spaceID, scheduleID, limit
	return append([]domainscheduledmessage.Delivery(nil), s.history...), nil
}

func (s *scheduledMessageRepoStub) UpdateActive(_ context.Context, spaceID, scheduleID string, fields dto.ScheduledMessageUpdateInput, next *domainscheduledmessage.Delivery, at time.Time) ([]string, error) {
	s.updatedFields, s.updatedAt, s.updateCalls = fields, at, s.updateCalls+1
	s.updatedSpaceID, s.updatedScheduleID = spaceID, scheduleID
	if next != nil {
		cloned := *next
		s.updatedDelivery = &cloned
		s.setPendingForIndex([]domainscheduledmessage.Delivery{cloned})
	} else {
		s.setPendingForIndex(nil)
	}
	s.afterPendingMutation()
	return append([]string(nil), s.updatedCancelledDeliveryIDs...), nil
}

func (s *scheduledMessageRepoStub) Cancel(context.Context, string, string, time.Time) ([]string, error) {
	s.cancelCalls++
	s.setPendingForIndex(nil)
	s.afterPendingMutation()
	return append([]string(nil), s.cancelledDeliveryIDs...), nil
}

func (s *scheduledMessageRepoStub) Unsubscribe(context.Context, string, string, string, time.Time) ([]string, error) {
	s.unsubscribeCalls++
	s.setPendingForIndex(nil)
	s.afterPendingMutation()
	return append([]string(nil), s.unsubscribedDeliveryIDs...), nil
}

type serviceSchedulerRepository struct {
	interfacescheduledmessage.SchedulerRepository
	serviceRepo *scheduledMessageRepoStub
}

func (r serviceSchedulerRepository) ListPendingForIndex(context.Context) ([]domainscheduledmessage.Delivery, error) {
	r.serviceRepo.pendingMu.Lock()
	defer r.serviceRepo.pendingMu.Unlock()
	return append([]domainscheduledmessage.Delivery(nil), r.serviceRepo.pendingForIndex...), nil
}

func (s *scheduledMessageRepoStub) BindCurrentGroup(_ context.Context, spaceID, memberID, channel, chatID, chatType string, _ time.Time) (*domainscheduledmessage.SpaceChannel, error) {
	s.bindCalls++
	s.bindSpaceID, s.bindMemberID, s.bindChannel, s.bindChatID, s.bindChatType = spaceID, memberID, channel, chatID, chatType
	if s.bound == nil {
		return &domainscheduledmessage.SpaceChannel{ID: serviceChannelID, SpaceID: spaceID, Channel: channel, Status: domainscheduledmessage.SpaceChannelStatusActive, BoundByMemberID: memberID}, nil
	}
	cloned := *s.bound
	return &cloned, nil
}

func (s *scheduledMessageRepoStub) ListTargets(context.Context, string) ([]dto.ScheduledMessageTargetOutput, error) {
	s.targetCalls++
	return append([]dto.ScheduledMessageTargetOutput(nil), s.targets...), nil
}

type scheduledMessageSpaceRepoStub struct {
	members []domainspace.ResolvedMembership
}

func (*scheduledMessageSpaceRepoStub) CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error {
	return nil
}
func (*scheduledMessageSpaceRepoStub) ListActiveByUserID(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
}
func (*scheduledMessageSpaceRepoStub) FindActiveMembership(context.Context, string, string) (*domainspace.ResolvedMembership, error) {
	return nil, nil
}
func (s *scheduledMessageSpaceRepoStub) ListActiveMembers(_ context.Context, spaceID string) ([]domainspace.ResolvedMembership, error) {
	members := make([]domainspace.ResolvedMembership, 0, len(s.members))
	for _, member := range s.members {
		if member.SpaceID == spaceID && member.Status == domainspace.StatusActive {
			members = append(members, member)
		}
	}
	return members, nil
}
func (*scheduledMessageSpaceRepoStub) Update(context.Context, string, domainspace.SpaceUpdateFields, time.Time) error {
	return nil
}
func (*scheduledMessageSpaceRepoStub) Archive(context.Context, string, time.Time) error { return nil }
func (*scheduledMessageSpaceRepoStub) UpdateMemberRole(context.Context, string, string, string, time.Time) error {
	return nil
}
func (*scheduledMessageSpaceRepoStub) RemoveMember(context.Context, string, string, time.Time) error {
	return nil
}
func (*scheduledMessageSpaceRepoStub) CountActiveOwners(context.Context, string) (int64, error) {
	return 1, nil
}

type scheduledMessageAuditStub struct {
	events []domainaudit.AuditEvent
}

func (s *scheduledMessageAuditStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (*scheduledMessageAuditStub) GetAll(context.Context, filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}

func (*scheduledMessageAuditStub) GetByID(context.Context, string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

type scheduledMessageIndexStub struct {
	upserts    []string
	removed    []string
	operations []string
	err        error
}

func (*scheduledMessageIndexStub) BeginLifecycleMutation()       {}
func (*scheduledMessageIndexStub) FinishLifecycleMutation(error) {}

func (s *scheduledMessageIndexStub) Upsert(_ context.Context, id string, _ time.Time) error {
	s.upserts = append(s.upserts, id)
	s.operations = append(s.operations, "upsert:"+id)
	return s.err
}
func (s *scheduledMessageIndexStub) Remove(_ context.Context, id string) error {
	s.removed = append(s.removed, id)
	s.operations = append(s.operations, "remove:"+id)
	return s.err
}

func newTestScheduledMessageService(repo *scheduledMessageRepoStub, spaces *scheduledMessageSpaceRepoStub, audit *scheduledMessageAuditStub, index interfacescheduledmessage.DueIndexWriter, generator interfacescheduledmessage.Generator, options ...ScheduledMessageServiceOptions) interfacescheduledmessage.Service {
	var auditDependency interfaceaudit.ServiceAuditInterface
	if audit != nil {
		auditDependency = audit
	}
	var indexDependency interfacescheduledmessage.DueIndexWriter
	if index != nil {
		indexDependency = index
	}
	var serviceOptions ScheduledMessageServiceOptions
	if len(options) > 0 {
		serviceOptions = options[0]
	}
	return NewScheduledMessageService(repo, spaces, authorization.NewAuthorizer(), auditDependency, indexDependency, generator, serviceOptions)
}

func TestServicePendingMutationsGuardIndexAcrossCommitToReconcileGap(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func() (*scheduledMessageRepoStub, *scheduledMessageSpaceRepoStub, func(interfacescheduledmessage.Service) error)
	}{
		{
			name: "create",
			setup: func() (*scheduledMessageRepoStub, *scheduledMessageSpaceRepoStub, func(interfacescheduledmessage.Service) error) {
				repo := &scheduledMessageRepoStub{}
				repo.targets = []dto.ScheduledMessageTargetOutput{{Kind: domainscheduledmessage.TargetDM, MemberID: stringPointer(serviceRecipientID), DisplayName: "Alex"}}
				spaces := lifecycleTestSpaces()
				actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")
				return repo, spaces, func(service interfacescheduledmessage.Service) error {
					_, err := service.Create(context.Background(), actor, validCreateInput())
					return err
				}
			},
		},
		{
			name: "update",
			setup: func() (*scheduledMessageRepoStub, *scheduledMessageSpaceRepoStub, func(interfacescheduledmessage.Service) error) {
				anchor := pastRecurringAnchor(t)
				repo := &scheduledMessageRepoStub{found: recurringScheduleFixture(serviceCreatorID, anchor, anchor.AddDate(0, 0, 7)), updatedCancelledDeliveryIDs: []string{serviceDeliveryID}}
				repo.setPendingForIndex([]domainscheduledmessage.Delivery{pendingServiceDelivery(serviceDeliveryID)})
				spaces := lifecycleTestSpaces()
				actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:update")
				content := "updated"
				return repo, spaces, func(service interfacescheduledmessage.Service) error {
					_, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: &content})
					return err
				}
			},
		},
		{
			name: "cancel",
			setup: func() (*scheduledMessageRepoStub, *scheduledMessageSpaceRepoStub, func(interfacescheduledmessage.Service) error) {
				anchor := pastRecurringAnchor(t)
				repo := &scheduledMessageRepoStub{found: recurringScheduleFixture(serviceCreatorID, anchor, anchor.AddDate(0, 0, 7)), cancelledDeliveryIDs: []string{serviceDeliveryID}}
				repo.setPendingForIndex([]domainscheduledmessage.Delivery{pendingServiceDelivery(serviceDeliveryID)})
				spaces := lifecycleTestSpaces()
				actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:delete")
				return repo, spaces, func(service interfacescheduledmessage.Service) error {
					_, err := service.Cancel(context.Background(), actor, serviceSpaceID, serviceScheduleID)
					return err
				}
			},
		},
		{
			name: "unsubscribe",
			setup: func() (*scheduledMessageRepoStub, *scheduledMessageSpaceRepoStub, func(interfacescheduledmessage.Service) error) {
				anchor := pastRecurringAnchor(t)
				repo := &scheduledMessageRepoStub{found: recurringScheduleFixture(serviceCreatorID, anchor, anchor.AddDate(0, 0, 7)), unsubscribedDeliveryIDs: []string{serviceDeliveryID}}
				repo.setPendingForIndex([]domainscheduledmessage.Delivery{pendingServiceDelivery(serviceDeliveryID)})
				spaces := lifecycleTestSpaces()
				actor := scheduledMessageActor(serviceSpaceID, serviceRecipientID, "user-recipient", "space_member", "scheduled_messages:unsubscribe")
				return repo, spaces, func(service interfacescheduledmessage.Service) error {
					_, err := service.Unsubscribe(context.Background(), actor, serviceSpaceID, serviceScheduleID)
					return err
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, spaces, mutate := test.setup()
			index := newTestDueIndex()
			service := newTestScheduledMessageService(repo, spaces, nil, index, nil)
			committed, releaseCommit := make(chan struct{}), make(chan struct{})
			repo.afterMutation = func() { close(committed); <-releaseCommit }
			markEntered, releaseMark := make(chan struct{}), make(chan struct{})
			index.beforeMark = func() { close(markEntered); <-releaseMark }
			clock := &testClock{now: time.Now().UTC()}
			scheduler, err := NewScheduledMessageScheduler(
				serviceSchedulerRepository{SchedulerRepository: newMemoryScheduledRepository(), serviceRepo: repo},
				index, nil, &testSender{}, testScheduledConfig(), dto.ScheduledMessageSchedulerOptions{Now: clock.time},
			)
			if err != nil {
				t.Fatalf("NewScheduledMessageScheduler() error = %v", err)
			}
			reconciled := make(chan error, 1)
			go func() { reconciled <- scheduler.reconcileIndex(context.Background()) }()
			<-markEntered
			mutated := make(chan error, 1)
			go func() { mutated <- mutate(service) }()
			select {
			case <-committed:
			case err := <-mutated:
				t.Fatalf("mutation returned before simulated commit/index gap: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("mutation did not reach the simulated DB commit/index gap")
			}
			close(releaseMark)
			if err := <-reconciled; err != nil {
				t.Fatalf("reconcileIndex() error = %v", err)
			}
			index.mu.Lock()
			degraded := index.degraded
			index.mu.Unlock()
			close(releaseCommit)
			if err := <-mutated; err != nil {
				t.Fatalf("mutation error = %v", err)
			}
			if !degraded {
				t.Fatal("reconciliation acknowledged a DB snapshot across a committed mutation whose cache write had not started")
			}
		})
	}
}

func lifecycleTestSpaces() *scheduledMessageSpaceRepoStub {
	return &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
}

func pendingServiceDelivery(id string) domainscheduledmessage.Delivery {
	next := time.Now().UTC().Add(time.Hour)
	return domainscheduledmessage.Delivery{ID: id, ScheduledMessageID: serviceScheduleID, ScheduledAt: next, NextAttemptAt: &next, Status: domainscheduledmessage.DeliveryStatusPending}
}

func scheduledMessageActor(spaceID, memberID, userID, role string, permissions ...string) domainidentity.ActorContext {
	permissionSet := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		permissionSet[permission] = struct{}{}
	}
	return domainidentity.ActorContext{
		UserID: userID, SpaceID: spaceID, MemberID: memberID, RoleName: role,
		Permissions: permissionSet, Source: "mcp", Channel: "whatsapp", ExternalProvider: "hermes",
		ExternalID: "sender@s.whatsapp.net",
	}
}

func scheduledMessageMember(spaceID, memberID, userID, name, role, status string) domainspace.ResolvedMembership {
	return domainspace.ResolvedMembership{
		ID: memberID, SpaceID: spaceID, UserID: userID, UserName: name, RoleName: role, Status: status,
	}
}

func validCreateInput() dto.ScheduledMessageCreateInput {
	recipientID := serviceRecipientID
	return dto.ScheduledMessageCreateInput{
		Space: serviceSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: &recipientID,
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "  Hello there.  ",
		Recurrence: domainscheduledmessage.RecurrenceOnce, Timezone: "UTC", StartAt: time.Now().UTC().Add(time.Hour),
	}
}

func recurringScheduleFixture(creatorID string, anchor, next time.Time) *domainscheduledmessage.Schedule {
	recipientID := serviceRecipientID
	return &domainscheduledmessage.Schedule{
		ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: creatorID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: &recipientID,
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "Original content",
		Recurrence: domainscheduledmessage.RecurrenceWeekly, Timezone: "Asia/Jakarta",
		StartAt: anchor, NextScheduledAt: &next, Status: domainscheduledmessage.ScheduleStatusActive,
	}
}

func pastRecurringAnchor(t *testing.T) time.Time {
	t.Helper()
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(location)
	oldDate := now.AddDate(-2, 0, 0)
	hour := (now.Hour() + 12) % 24
	return time.Date(oldDate.Year(), oldDate.Month(), oldDate.Day(), hour, 17, 0, 0, location)
}

func nextCalendarOccurrence(anchor, after time.Time, recurrence domainscheduledmessage.Recurrence) time.Time {
	days := 1
	if recurrence == domainscheduledmessage.RecurrenceWeekly {
		days = 7
	}
	next := anchor
	for !next.After(after) {
		next = next.AddDate(0, 0, days)
	}
	return next
}

func TestCreateRejectsCrossSpaceAccess(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	input := validCreateInput()
	input.Space = otherServiceSpaceID
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	if _, err := service.Create(context.Background(), actor, input); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("create cross-space schedule error = %v, want not found", err)
	}
	if repo.created != nil {
		t.Fatal("created a schedule in another Space")
	}
}

func TestCreateRejectsInactiveSender(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusInactive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	if _, err := service.Create(context.Background(), actor, validCreateInput()); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("create with inactive sender error = %v, want not found", err)
	}
	if repo.created != nil {
		t.Fatal("created a schedule for an inactive sender")
	}
}

func TestCreateRejectsInactiveRecipient(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusInactive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	if _, err := service.Create(context.Background(), actor, validCreateInput()); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("create with inactive recipient error = %v, want not found", err)
	}
	if repo.created != nil {
		t.Fatal("created a schedule for an inactive recipient")
	}
}

func TestCreateUsesRepositoryWhatsAppEligibilityAcrossAllIdentities(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, otherRecipientID, "user-other", "Alex", "space_member", domainspace.StatusActive),
	}}
	// ListTargets represents the all-identities query: this member can also have a
	// Telegram identity, but one verified WhatsApp identity makes the member eligible.
	repo.targets = []dto.ScheduledMessageTargetOutput{
		{Kind: domainscheduledmessage.TargetDM, MemberID: stringPointer(serviceRecipientID), DisplayName: "Alex"},
		{Kind: domainscheduledmessage.TargetDM, MemberID: stringPointer(otherRecipientID), DisplayName: "Alex"},
	}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	if _, err := service.Create(context.Background(), actor, validCreateInput()); err != nil {
		t.Fatalf("create for member with a verified WhatsApp identity among multiple Hermes identities: %v", err)
	}
	if repo.created == nil || repo.created.RecipientMemberID == nil || *repo.created.RecipientMemberID != serviceRecipientID || repo.targetCalls != 1 {
		t.Fatalf("selected recipient = %#v; repository target lookups = %d", repo.created, repo.targetCalls)
	}
}

func TestCreateRejectsRawNumericIdentityExcludedByRepositoryTargets(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	if _, err := service.Create(context.Background(), actor, validCreateInput()); !errors.Is(err, interfacescheduledmessage.ErrInvalidWhatsAppIdentity) {
		t.Fatalf("create when only a raw numeric identity exists outside the eligible target list = %v, want invalid WhatsApp identity", err)
	}
	if repo.created != nil {
		t.Fatal("created a schedule for a member without an eligible WhatsApp identity")
	}
}

func TestCreateDoesNotFallBackToNameWhenExactMemberTargetIsMissing(t *testing.T) {
	repo := &scheduledMessageRepoStub{targets: []dto.ScheduledMessageTargetOutput{
		{Kind: domainscheduledmessage.TargetDM, MemberID: stringPointer(otherRecipientID), DisplayName: "Alex"},
	}}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, otherRecipientID, "user-other", "Alex", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	if _, err := service.Create(context.Background(), actor, validCreateInput()); !errors.Is(err, interfacescheduledmessage.ErrInvalidWhatsAppIdentity) {
		t.Fatalf("create without exact member target error = %v, want invalid WhatsApp identity", err)
	}
	if repo.created != nil {
		t.Fatal("fell back to another member with the same name")
	}
}

func TestCreateRejectsRawJIDAsRecipientIdentifier(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	input := validCreateInput()
	rawJID := "628123456789@s.whatsapp.net"
	input.RecipientMemberID = &rawJID
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	var validation *authorization.ValidationError
	if _, err := service.Create(context.Background(), actor, input); !errors.As(err, &validation) || validation.Field != "recipient_member_id" {
		t.Fatalf("create with raw JID error = %v, want recipient ID validation", err)
	}
	if repo.created != nil {
		t.Fatal("accepted raw JID as a recipient identifier")
	}
}

func TestBindCurrentGroupRejectsNonAdmin(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_member", "space_channels:create")
	input := dto.SpaceChannelBindInput{Space: serviceSpaceID, CurrentChannel: "whatsapp", CurrentChatID: "120363123456@g.us", CurrentChatType: "group"}

	if _, err := service.BindCurrentGroup(context.Background(), actor, input); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("bind group as member error = %v, want forbidden", err)
	}
	if repo.bindCalls != 0 {
		t.Fatal("passed unauthorized group binding to repository")
	}
}

func TestBindCurrentGroupUsesCurrentGroupContextNotSenderIdentity(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_admin", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_admin", "space_channels:create")
	input := dto.SpaceChannelBindInput{Space: serviceSpaceID, CurrentChannel: "whatsapp", CurrentChatID: "120363123456@g.us", CurrentChatType: "group"}

	if _, err := service.BindCurrentGroup(context.Background(), actor, input); err != nil {
		t.Fatalf("bind current verified group: %v", err)
	}
	if repo.bindCalls != 1 || repo.bindChatID != input.CurrentChatID || repo.bindChatID == actor.ExternalID || repo.bindChatType != "group" {
		t.Fatalf("repository group context = %#v, want current group context", repo)
	}
}

func TestUnsubscribeAlreadyUnsubscribedIsIdempotent(t *testing.T) {
	recipientID := serviceRecipientID
	schedule := &domainscheduledmessage.Schedule{
		ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: serviceCreatorID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: &recipientID,
		Status: domainscheduledmessage.ScheduleStatusUnsubscribed, UnsubscribedByMemberID: &recipientID,
	}
	repo := &scheduledMessageRepoStub{found: schedule}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceRecipientID, "user-recipient", "space_member", "scheduled_messages:unsubscribe")

	if _, err := service.Unsubscribe(context.Background(), actor, serviceSpaceID, serviceScheduleID); err != nil {
		t.Fatalf("repeat unsubscribe: %v", err)
	}
	if repo.unsubscribeCalls != 0 {
		t.Fatal("repeated unsubscribe mutated a terminal schedule")
	}
}

func TestCancelRemovesCancelledDeliveryUUIDs(t *testing.T) {
	schedule := &domainscheduledmessage.Schedule{
		ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: serviceCreatorID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(serviceRecipientID),
		Status: domainscheduledmessage.ScheduleStatusActive,
	}
	repo := &scheduledMessageRepoStub{found: schedule, cancelledDeliveryIDs: []string{serviceDeliveryID, serviceOtherDeliveryID}}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
	}}
	index := &scheduledMessageIndexStub{}
	service := newTestScheduledMessageService(repo, spaces, nil, index, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:delete")

	if _, err := service.Cancel(context.Background(), actor, serviceSpaceID, serviceScheduleID); err != nil {
		t.Fatalf("cancel active schedule: %v", err)
	}
	if len(index.operations) != 2 || index.operations[0] != "remove:"+serviceDeliveryID || index.operations[1] != "remove:"+serviceOtherDeliveryID {
		t.Fatalf("due-index removals = %v, want cancelled delivery UUIDs", index.operations)
	}
}

func TestUnsubscribeRemovesCancelledDeliveryUUID(t *testing.T) {
	schedule := &domainscheduledmessage.Schedule{
		ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: serviceCreatorID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(serviceRecipientID),
		Status: domainscheduledmessage.ScheduleStatusActive,
	}
	repo := &scheduledMessageRepoStub{found: schedule, unsubscribedDeliveryIDs: []string{serviceDeliveryID}}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
	index := &scheduledMessageIndexStub{}
	service := newTestScheduledMessageService(repo, spaces, nil, index, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceRecipientID, "user-recipient", "space_member", "scheduled_messages:unsubscribe")

	if _, err := service.Unsubscribe(context.Background(), actor, serviceSpaceID, serviceScheduleID); err != nil {
		t.Fatalf("unsubscribe active DM schedule: %v", err)
	}
	if len(index.operations) != 1 || index.operations[0] != "remove:"+serviceDeliveryID {
		t.Fatalf("due-index removals = %v, want cancelled delivery UUID", index.operations)
	}
}

func TestUpdateCannotReactivateUnsubscribedSchedule(t *testing.T) {
	recipientID := serviceRecipientID
	schedule := &domainscheduledmessage.Schedule{
		ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: serviceCreatorID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: &recipientID,
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "Hello", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: time.Now().UTC().Add(time.Hour), Status: domainscheduledmessage.ScheduleStatusUnsubscribed,
	}
	repo := &scheduledMessageRepoStub{found: schedule}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	content := "Changed"
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:update")

	if _, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: &content}); !errors.Is(err, interfacescheduledmessage.ErrConflict) {
		t.Fatalf("update unsubscribed schedule error = %v, want conflict", err)
	}
	if repo.updateCalls != 0 {
		t.Fatal("updated a terminal schedule")
	}
}

func TestUpdateRecurringScheduleReplacesDeliveryAndReindexesAfterCalendarChange(t *testing.T) {
	anchor := pastRecurringAnchor(t)
	staleNext := anchor.AddDate(0, 0, 7)
	schedule := recurringScheduleFixture(otherRecipientID, anchor, staleNext)
	repo := &scheduledMessageRepoStub{found: schedule, updatedCancelledDeliveryIDs: []string{serviceDeliveryID}}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-owner", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, otherRecipientID, "user-other", "Alex", "space_member", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Lee", "space_member", domainspace.StatusActive),
	}}
	index := &scheduledMessageIndexStub{}
	service := newTestScheduledMessageService(repo, spaces, nil, index, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-owner", "space_owner", "scheduled_messages:update")
	content := "Updated content"
	recurrence := domainscheduledmessage.RecurrenceDaily
	before := time.Now()

	updated, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{
		Content: &content, Recurrence: &recurrence,
	})
	if err != nil {
		t.Fatalf("update active recurring schedule as owner: %v", err)
	}
	expected := nextCalendarOccurrence(anchor, before, recurrence)
	if updated.NextScheduledAt == nil || !updated.NextScheduledAt.Equal(expected) {
		t.Fatalf("updated next occurrence = %v, want calendar occurrence %v", updated.NextScheduledAt, expected)
	}
	if updated.SpaceID != serviceSpaceID || updated.CreatedByMemberID != otherRecipientID || updated.RecipientMemberID == nil || *updated.RecipientMemberID != serviceRecipientID || !updated.StartAt.Equal(anchor) {
		t.Fatalf("update changed schedule identity/anchor: %#v", updated)
	}
	if repo.updateCalls != 1 || repo.updatedSpaceID != serviceSpaceID || repo.updatedScheduleID != serviceScheduleID {
		t.Fatalf("repository update scope/calls = %d/%s/%s", repo.updateCalls, repo.updatedSpaceID, repo.updatedScheduleID)
	}
	if repo.updatedDelivery == nil || repo.updatedDelivery.ScheduledMessageID != serviceScheduleID || !repo.updatedDelivery.ScheduledAt.Equal(expected) || repo.updatedDelivery.NextAttemptAt == nil || !repo.updatedDelivery.NextAttemptAt.Equal(expected) || repo.updatedDelivery.Status != domainscheduledmessage.DeliveryStatusPending || repo.updatedDelivery.ContentSnapshot == nil || *repo.updatedDelivery.ContentSnapshot != content {
		t.Fatalf("replacement delivery = %#v, want pending at %v with new content snapshot", repo.updatedDelivery, expected)
	}
	if repo.updatedDelivery == nil || repo.updatedDelivery.ID == serviceScheduleID || len(index.operations) != 2 || index.operations[0] != "remove:"+serviceDeliveryID || index.operations[1] != "upsert:"+repo.updatedDelivery.ID {
		t.Fatalf("due-index operations = %v for schedule %s and replacement delivery %s, want canceled then replacement delivery UUIDs", index.operations, serviceScheduleID, repo.updatedDelivery.ID)
	}
}

func TestUpdateContentOnlyRecomputesFutureOccurrenceFromPastAnchor(t *testing.T) {
	anchor := pastRecurringAnchor(t)
	staleNext := anchor.AddDate(0, 0, 7)
	repo := &scheduledMessageRepoStub{found: recurringScheduleFixture(serviceCreatorID, anchor, staleNext)}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_member", "scheduled_messages:update")
	content := "Fresh copy"
	before := time.Now()

	updated, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: &content})
	if err != nil {
		t.Fatalf("content-only update of recurring schedule with a past anchor: %v", err)
	}
	expected := nextCalendarOccurrence(anchor, before, domainscheduledmessage.RecurrenceWeekly)
	if updated.NextScheduledAt == nil || !updated.NextScheduledAt.Equal(expected) || !updated.StartAt.Equal(anchor) {
		t.Fatalf("content-only update next/anchor = %v/%v, want next %v and unchanged anchor %v", updated.NextScheduledAt, updated.StartAt, expected, anchor)
	}
	if repo.updatedDelivery == nil || !repo.updatedDelivery.ScheduledAt.Equal(expected) {
		t.Fatalf("replacement delivery = %#v, want future occurrence %v", repo.updatedDelivery, expected)
	}
}

func TestGroupCreatorCanUpdateOwnScheduleWithoutChangingDestination(t *testing.T) {
	anchor := pastRecurringAnchor(t)
	schedule := recurringScheduleFixture(serviceCreatorID, anchor, anchor.AddDate(0, 0, 7))
	schedule.Target, schedule.RecipientMemberID, schedule.SpaceChannelID = domainscheduledmessage.TargetGroup, nil, stringPointer(serviceChannelID)
	repo := &scheduledMessageRepoStub{found: schedule}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_member", "scheduled_messages:update")
	content := "Updated group message"

	updated, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: &content})
	if err != nil {
		t.Fatalf("group creator updating own schedule: %v", err)
	}
	if repo.updateCalls != 1 || updated.CreatedByMemberID != serviceCreatorID || updated.Target != domainscheduledmessage.TargetGroup || updated.SpaceChannelID == nil || *updated.SpaceChannelID != serviceChannelID || updated.RecipientMemberID != nil {
		t.Fatalf("group update changed ownership/destination or missed persistence: %#v; update calls=%d", updated, repo.updateCalls)
	}
}

func TestGroupUpdateRejectsUnrelatedOrdinaryMember(t *testing.T) {
	anchor := pastRecurringAnchor(t)
	schedule := recurringScheduleFixture(serviceCreatorID, anchor, anchor.AddDate(0, 0, 7))
	schedule.Target, schedule.RecipientMemberID, schedule.SpaceChannelID = domainscheduledmessage.TargetGroup, nil, stringPointer(serviceChannelID)
	repo := &scheduledMessageRepoStub{found: schedule}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_member", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Lee", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceRecipientID, "user-recipient", "space_member", "scheduled_messages:update")
	content := "Unauthorized change"

	if _, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: &content}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("unrelated group member update error = %v, want forbidden", err)
	}
	if repo.updateCalls != 0 {
		t.Fatal("persisted an unrelated member's group update")
	}
}

func TestUpdateRejectsExplicitPastStartAndUnrelatedOrdinaryMember(t *testing.T) {
	anchor := pastRecurringAnchor(t)
	schedule := recurringScheduleFixture(serviceCreatorID, anchor, anchor.AddDate(0, 0, 7))
	content := "Changed"
	pastStart := anchor
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_member", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Lee", "space_member", domainspace.StatusActive),
	}}
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_member", "scheduled_messages:update")

	repo := &scheduledMessageRepoStub{found: schedule}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	var validation *authorization.ValidationError
	if _, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{StartAt: &pastStart}); !errors.As(err, &validation) || validation.Field != "start_at" {
		t.Fatalf("explicit past start error = %v, want start_at validation", err)
	}
	if repo.updateCalls != 0 {
		t.Fatal("persisted an explicitly supplied past start")
	}

	repo.found = schedule
	service = newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	otherActor := scheduledMessageActor(serviceSpaceID, serviceRecipientID, "user-recipient", "space_member", "scheduled_messages:update")
	if _, err := service.Update(context.Background(), otherActor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: &content}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("unrelated ordinary member update error = %v, want forbidden", err)
	}
	if repo.updateCalls != 0 {
		t.Fatal("allowed an unrelated ordinary member to update the schedule")
	}
}

func TestCreateAcceptsFixedTextWithoutGeneratorAndAuditsCorrelationID(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
	repo.targets = []dto.ScheduledMessageTargetOutput{{Kind: domainscheduledmessage.TargetDM, MemberID: stringPointer(serviceRecipientID), DisplayName: "Alex"}}
	audit := &scheduledMessageAuditStub{}
	index := &scheduledMessageIndexStub{err: errors.New("secret cache details")}
	service := newTestScheduledMessageService(repo, spaces, audit, index, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")
	ctx := logger.WithLogMetadata(context.Background(), "request-123", "")

	if _, err := service.Create(ctx, actor, validCreateInput()); err != nil {
		t.Fatalf("create fixed text without generator: %v", err)
	}
	if repo.created == nil || repo.firstDelivery == nil || repo.created.ID == repo.firstDelivery.ID || len(index.upserts) != 1 || index.upserts[0] != repo.firstDelivery.ID {
		t.Fatalf("created schedule/delivery/index = %#v/%#v/%v; due index must use delivery UUID", repo.created, repo.firstDelivery, index.upserts)
	}
	if len(audit.events) != 1 || audit.events[0].RequestID != "request-123" {
		t.Fatalf("audit events = %#v, want correlation ID", audit.events)
	}
	metadata, err := json.Marshal(audit.events[0].Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), "Hello there") || strings.Contains(string(metadata), "@s.whatsapp.net") || strings.Contains(string(metadata), "secret cache details") {
		t.Fatalf("audit metadata was not sanitized: %s", metadata)
	}
}

func TestCreateRejectsAIWhenGeneratorUnavailable(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	input := validCreateInput()
	input.ContentMode = domainscheduledmessage.ContentModeAI
	input.Content = "Make it gentle"
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")

	var validation *authorization.ValidationError
	if _, err := service.Create(context.Background(), actor, input); !errors.As(err, &validation) || validation.Field != "content_mode" {
		t.Fatalf("create AI without generator error = %v, want content mode validation", err)
	}
	if repo.created != nil {
		t.Fatal("created AI schedule without a generator")
	}
}

func TestCreateUsesConfiguredTextLimit(t *testing.T) {
	repo := &scheduledMessageRepoStub{}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil, ScheduledMessageServiceOptions{MaxTextBytes: 4})
	input := validCreateInput()
	input.Content = "hello"
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:create")
	var validation *authorization.ValidationError
	if _, err := service.Create(context.Background(), actor, input); !errors.As(err, &validation) || validation.Field != "content" {
		t.Fatalf("create over configured text limit error = %v, want content validation", err)
	}
	if repo.created != nil {
		t.Fatal("created a schedule with content over the configured limit")
	}
}

func TestUpdateUsesConfiguredTextLimit(t *testing.T) {
	for _, test := range []struct {
		name      string
		maxBytes  int
		content   string
		wantError bool
	}{
		{name: "default limit", content: strings.Repeat("x", 2001), wantError: true},
		{name: "configured smaller limit", maxBytes: 4, content: "12345", wantError: true},
		{name: "configured larger limit", maxBytes: 4096, content: strings.Repeat("x", 3000)},
	} {
		t.Run(test.name, func(t *testing.T) {
			runUpdateTextLimitCase(t, test.maxBytes, test.content, test.wantError)
		})
	}
}

func runUpdateTextLimitCase(t *testing.T, maxBytes int, content string, wantError bool) {
	t.Helper()
	anchor := pastRecurringAnchor(t)
	repo := &scheduledMessageRepoStub{found: recurringScheduleFixture(serviceCreatorID, anchor, anchor.AddDate(0, 0, 7))}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil, ScheduledMessageServiceOptions{MaxTextBytes: maxBytes})
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_member", "scheduled_messages:update")
	_, err := service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: &content})
	if wantError {
		var validation *authorization.ValidationError
		if !errors.As(err, &validation) || validation.Field != "content" {
			t.Fatalf("update over configured text limit error = %v, want content validation", err)
		}
		if repo.updateCalls != 0 {
			t.Fatal("updated a schedule with content over the configured limit")
		}
		return
	}
	if err != nil {
		t.Fatalf("update under configured text limit: %v", err)
	}
	if repo.updateCalls != 1 || repo.updatedFields.Content == nil || *repo.updatedFields.Content != content {
		t.Fatalf("update fields = %+v after %d calls, want configured content persisted", repo.updatedFields, repo.updateCalls)
	}
}

func TestListFiltersPrivateDMAndDoesNotReadHistoryForOrdinaryList(t *testing.T) {
	ownedByOther := serviceRecipientID
	listed := []domainscheduledmessage.Schedule{
		{ID: serviceScheduleID, SpaceID: serviceSpaceID, Target: domainscheduledmessage.TargetDM, CreatedByMemberID: serviceCreatorID, RecipientMemberID: &ownedByOther},
		{ID: "00000000-0000-4000-8000-000000000102", SpaceID: serviceSpaceID, Target: domainscheduledmessage.TargetDM, CreatedByMemberID: serviceCreatorID, RecipientMemberID: &ownedByOther},
		{ID: "00000000-0000-4000-8000-000000000103", SpaceID: serviceSpaceID, Target: domainscheduledmessage.TargetGroup, CreatedByMemberID: serviceCreatorID},
	}
	listed[1].RecipientMemberID = stringPointer(otherRecipientID)
	repo := &scheduledMessageRepoStub{listed: listed}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceRecipientID, "user-recipient", "space_member", "scheduled_messages:list")

	got, err := service.List(context.Background(), actor, dto.ScheduledMessageListInput{Space: serviceSpaceID})
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(got) != 2 || got[0].ID != serviceScheduleID || len(repo.history) != 0 || repo.listInput.ViewerMemberID != serviceRecipientID || repo.listInput.CanViewAllDMs {
		t.Fatalf("listed schedules = %#v; repository filter = %#v", got, repo.listInput)
	}
}

func TestListUsesConfiguredLimitAndIgnoresCallerLimit(t *testing.T) {
	repo := &scheduledMessageRepoStub{listed: []domainscheduledmessage.Schedule{
		{ID: serviceScheduleID, SpaceID: serviceSpaceID, Target: domainscheduledmessage.TargetGroup, CreatedByMemberID: serviceCreatorID},
		{ID: "00000000-0000-4000-8000-000000000102", SpaceID: serviceSpaceID, Target: domainscheduledmessage.TargetGroup, CreatedByMemberID: serviceCreatorID},
	}}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil, ScheduledMessageServiceOptions{MaxListRows: 1})
	actor := scheduledMessageActor(serviceSpaceID, serviceCreatorID, "user-creator", "space_owner", "scheduled_messages:list")
	got, err := service.List(context.Background(), actor, dto.ScheduledMessageListInput{Space: serviceSpaceID, Limit: 100})
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(got) != 1 || repo.listInput.Limit != 1 || repo.listInput.ViewerMemberID != serviceCreatorID || !repo.listInput.CanViewAllDMs {
		t.Fatalf("listed %d schedules with trusted repository input %+v", len(got), repo.listInput)
	}
}

func TestTargetedListReturnsAtMostOneHundredHistoryRowsWithoutLease(t *testing.T) {
	recipientID := serviceRecipientID
	schedule := domainscheduledmessage.Schedule{
		ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: serviceCreatorID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: &recipientID,
	}
	lease := "must-not-leak"
	repo := &scheduledMessageRepoStub{listed: []domainscheduledmessage.Schedule{schedule}}
	for i := 0; i < 105; i++ {
		repo.history = append(repo.history, domainscheduledmessage.Delivery{
			ID: "00000000-0000-4000-8000-000000000401", ScheduledMessageID: serviceScheduleID,
			ScheduledAt: time.Now().UTC(), Status: domainscheduledmessage.DeliveryStatusPending, LeaseToken: &lease,
		})
	}
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	actor := scheduledMessageActor(serviceSpaceID, serviceRecipientID, "user-recipient", "space_member", "scheduled_messages:view")
	got, err := service.List(context.Background(), actor, dto.ScheduledMessageListInput{Space: serviceSpaceID, ScheduleID: &schedule.ID})
	if err != nil {
		t.Fatalf("list schedule history: %v", err)
	}
	if len(got) != 1 || len(got[0].History) != 100 || repo.historyLimit != 100 || repo.historySpaceID != serviceSpaceID || repo.historyScheduleID != serviceScheduleID {
		t.Fatalf("targeted result/history = %#v; history request = %d/%s/%s", got, repo.historyLimit, repo.historySpaceID, repo.historyScheduleID)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "lease_token") || strings.Contains(string(encoded), "external_chat_id") {
		t.Fatalf("history output exposed internal delivery fields: %s", encoded)
	}
}

type canonicalPermissionCase struct {
	name     string
	resource string
	action   string
	memberID string
	role     string
}

func TestCanonicalResourceActionPermissionsAuthorizeScheduledMessageService(t *testing.T) {
	tests := []canonicalPermissionCase{
		{name: "create and targets", resource: "scheduled_messages", action: "create", memberID: serviceCreatorID, role: "space_owner"},
		{name: "list", resource: "scheduled_messages", action: "list", memberID: serviceCreatorID, role: "space_owner"},
		{name: "view", resource: "scheduled_messages", action: "view", memberID: serviceCreatorID, role: "space_owner"},
		{name: "update", resource: "scheduled_messages", action: "update", memberID: serviceCreatorID, role: "space_owner"},
		{name: "cancel", resource: "scheduled_messages", action: "delete", memberID: serviceCreatorID, role: "space_owner"},
		{name: "unsubscribe", resource: "scheduled_messages", action: "unsubscribe", memberID: serviceRecipientID, role: "space_member"},
		{name: "bind group", resource: "space_channels", action: "create", memberID: serviceCreatorID, role: "space_owner"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { runCanonicalPermissionCase(t, test) })
	}
}

func runCanonicalPermissionCase(t *testing.T, test canonicalPermissionCase) {
	t.Helper()
	repo := &scheduledMessageRepoStub{}
	prepareCanonicalPermissionCase(repo, test.name)
	spaces := &scheduledMessageSpaceRepoStub{members: []domainspace.ResolvedMembership{
		scheduledMessageMember(serviceSpaceID, serviceCreatorID, "user-creator", "Sam", "space_owner", domainspace.StatusActive),
		scheduledMessageMember(serviceSpaceID, serviceRecipientID, "user-recipient", "Alex", "space_member", domainspace.StatusActive),
	}}
	service := newTestScheduledMessageService(repo, spaces, nil, nil, nil)
	permission := authscope.PermissionKey(test.resource, test.action)
	actor := scheduledMessageActor(serviceSpaceID, test.memberID, "user-creator", test.role, permission)
	if test.memberID == serviceRecipientID {
		actor.UserID = "user-recipient"
	}
	runCanonicalPermissionCall(t, test, service, actor)
}

func prepareCanonicalPermissionCase(repo *scheduledMessageRepoStub, name string) {
	switch name {
	case "create and targets":
		repo.targets = []dto.ScheduledMessageTargetOutput{{Kind: domainscheduledmessage.TargetDM, MemberID: stringPointer(serviceRecipientID)}}
	case "view":
		repo.listed = []domainscheduledmessage.Schedule{{ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: serviceCreatorID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(serviceRecipientID)}}
	case "update", "cancel", "unsubscribe":
		repo.found = canonicalPermissionSchedule()
	}
}

func runCanonicalPermissionCall(t *testing.T, test canonicalPermissionCase, service interfacescheduledmessage.Service, actor domainidentity.ActorContext) {
	t.Helper()
	var err error
	switch test.name {
	case "create and targets":
		_, err = service.Targets(context.Background(), actor, serviceSpaceID)
		if err == nil {
			_, err = service.Create(context.Background(), actor, validCreateInput())
		}
	case "list":
		_, err = service.List(context.Background(), actor, dto.ScheduledMessageListInput{Space: serviceSpaceID})
	case "view":
		_, err = service.List(context.Background(), actor, dto.ScheduledMessageListInput{Space: serviceSpaceID, ScheduleID: stringPointer(serviceScheduleID)})
	case "update":
		_, err = service.Update(context.Background(), actor, serviceSpaceID, serviceScheduleID, dto.ScheduledMessageUpdateInput{Content: stringPointer("updated")})
	case "cancel":
		_, err = service.Cancel(context.Background(), actor, serviceSpaceID, serviceScheduleID)
	case "unsubscribe":
		_, err = service.Unsubscribe(context.Background(), actor, serviceSpaceID, serviceScheduleID)
	case "bind group":
		input := dto.SpaceChannelBindInput{Space: serviceSpaceID, CurrentChannel: "whatsapp", CurrentChatID: "120363123456@g.us", CurrentChatType: "group"}
		_, err = service.BindCurrentGroup(context.Background(), actor, input)
	}
	if err != nil {
		t.Fatalf("%s with canonical %s:%s permission: %v", test.name, test.resource, test.action, err)
	}
}

func canonicalPermissionSchedule() *domainscheduledmessage.Schedule {
	startAt := time.Now().UTC().Add(time.Hour)
	return &domainscheduledmessage.Schedule{
		ID: serviceScheduleID, SpaceID: serviceSpaceID, CreatedByMemberID: serviceCreatorID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(serviceRecipientID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "original", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: startAt, NextScheduledAt: &startAt, Status: domainscheduledmessage.ScheduleStatusActive,
	}
}

func stringPointer(value string) *string { return &value }

var _ interfacespace.RepoSpaceInterface = (*scheduledMessageSpaceRepoStub)(nil)
var _ interfacescheduledmessage.Repository = (*scheduledMessageRepoStub)(nil)
