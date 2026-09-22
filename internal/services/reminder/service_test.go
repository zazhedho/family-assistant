package servicereminder

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	"family-assistant/internal/services/authorization"
	"gorm.io/gorm"
)

var _ interfacereminder.ServiceReminderInterface = (*service)(nil)
var _ domainidentity.ActorContext = domainidentity.ActorContext{}

const (
	personalSpaceID = "space-personal"
	sharedSpaceID   = "space-shared"
	creatorID       = "member-creator"
	assigneeID      = "member-assignee"
	reminderID      = "00000000-0000-0000-0000-000000000101"
)

type reminderRepositoryStub struct {
	created      *domainreminder.Reminder
	listed       []domainreminder.Reminder
	listFilter   domainreminder.ListFilter
	found        *domainreminder.Reminder
	findErr      error
	findSpace    string
	findID       string
	completed    bool
	completeErr  error
	updated      bool
	updateSpace  string
	updateID     string
	updatePatch  domainreminder.UpdateFields
	updateErr    error
	deleted      bool
	deleteSpace  string
	deleteID     string
	deleteStatus domainreminder.Status
	deleteErr    error
}

func (s *reminderRepositoryStub) Create(_ context.Context, reminder *domainreminder.Reminder) error {
	copy := *reminder
	s.created = &copy
	return nil
}

func (s *reminderRepositoryStub) FindByIDInSpace(_ context.Context, spaceID, reminderID string) (*domainreminder.Reminder, error) {
	s.findSpace, s.findID = spaceID, reminderID
	if s.findErr != nil {
		return nil, s.findErr
	}
	if s.found == nil {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *s.found
	return &copy, nil
}

func (s *reminderRepositoryStub) List(_ context.Context, filter domainreminder.ListFilter) ([]domainreminder.Reminder, error) {
	s.listFilter = filter
	return append([]domainreminder.Reminder(nil), s.listed...), nil
}

func (s *reminderRepositoryStub) CompletePending(_ context.Context, _, _ string, _ time.Time) error {
	if s.completeErr != nil {
		return s.completeErr
	}
	s.completed = true
	return nil
}

func (s *reminderRepositoryStub) UpdatePending(_ context.Context, spaceID, reminderID string, patch domainreminder.UpdateFields, _ time.Time) error {
	s.updateSpace, s.updateID, s.updatePatch = spaceID, reminderID, patch
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updated = true
	return nil
}

func (s *reminderRepositoryStub) SoftDelete(_ context.Context, spaceID, reminderID string, status domainreminder.Status, _ time.Time) error {
	s.deleteSpace, s.deleteID, s.deleteStatus = spaceID, reminderID, status
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = true
	return nil
}

type spaceRepositoryStub struct {
	members []domainspace.ResolvedMembership
}

func (s *spaceRepositoryStub) CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error {
	return nil
}

func (s *spaceRepositoryStub) ListActiveByUserID(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
}

func (s *spaceRepositoryStub) FindActiveMembership(context.Context, string, string) (*domainspace.ResolvedMembership, error) {
	return nil, nil
}

func (s *spaceRepositoryStub) ListActiveMembers(_ context.Context, spaceID string) ([]domainspace.ResolvedMembership, error) {
	result := make([]domainspace.ResolvedMembership, 0, len(s.members))
	for _, member := range s.members {
		if member.SpaceID == spaceID && member.Status == domainspace.StatusActive {
			result = append(result, member)
		}
	}
	return result, nil
}

type auditStoreStub struct {
	events []domainaudit.AuditEvent
}

func (s *auditStoreStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func actor(spaceID, spaceType, memberID, role string, permissions ...string) domainidentity.ActorContext {
	permissionSet := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		permissionSet[permission] = struct{}{}
	}
	return domainidentity.ActorContext{
		UserID:           "user-1",
		SpaceID:          spaceID,
		SpaceType:        spaceType,
		MemberID:         memberID,
		RoleName:         role,
		Permissions:      permissionSet,
		Source:           " MCP ",
		Channel:          " whatsapp ",
		ExternalProvider: "hermes",
		ExternalID:       " profile-1 ",
	}
}

func membership(spaceID, memberID, role, userID string) domainspace.ResolvedMembership {
	return domainspace.ResolvedMembership{
		ID:       memberID,
		SpaceID:  spaceID,
		UserID:   userID,
		RoleName: role,
		Status:   domainspace.StatusActive,
	}
}

func newReminderService(repo *reminderRepositoryStub, spaces *spaceRepositoryStub, audit *auditStoreStub) interfacereminder.ServiceReminderInterface {
	if audit == nil {
		return NewReminderService(repo, spaces, authorization.NewAuthorizer(), nil)
	}
	return NewReminderService(repo, spaces, authorization.NewAuthorizer(), audit)
}

func TestCreateDefaultsToActorPersonalSpace(t *testing.T) {
	repo := &reminderRepositoryStub{}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(personalSpaceID, creatorID, "space_owner", "user-1"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(personalSpaceID, domainspace.TypePersonal, creatorID, "space_owner", "reminders:create")

	created, err := service.Create(context.Background(), actor, dto.ReminderCreateInput{
		Title:       "Pay electricity bill",
		ScheduledAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if created == nil || repo.created == nil {
		t.Fatal("expected created reminder")
	}
	if repo.created.SpaceID != personalSpaceID || repo.created.CreatedByMemberID != creatorID {
		t.Fatalf("created reminder = %#v, want personal Space and creator", repo.created)
	}
	if repo.created.Status != domainreminder.StatusPending {
		t.Fatalf("status = %q, want %q", repo.created.Status, domainreminder.StatusPending)
	}
}

func TestCreateRejectsAssigneeOutsideActorSpaceAsNotFound(t *testing.T) {
	repo := &reminderRepositoryStub{}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_member", "user-1"),
		membership("space-other", assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_member", "reminders:create")

	_, err := service.Create(context.Background(), actor, dto.ReminderCreateInput{
		Title:            "Review budget",
		ScheduledAt:      time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
		AssigneeMemberID: stringPtr(assigneeID),
	})
	if !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("create error = %v, want not found", err)
	}
	if repo.created != nil {
		t.Fatal("created reminder with cross-space assignee")
	}
}

func TestListSharedReturnsRemindersForActiveMemberWithoutAssignmentFilter(t *testing.T) {
	assigned := assigneeID
	repo := &reminderRepositoryStub{listed: []domainreminder.Reminder{
		{ID: "reminder-owned", SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, Status: domainreminder.StatusPending},
		{ID: "reminder-assigned", SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, AssigneeMemberID: &assigned, Status: domainreminder.StatusPending},
	}}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
		membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_owner", "reminders:list")

	got, err := service.List(context.Background(), actor, dto.ReminderListInput{Space: sharedSpaceID})
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(got) != 2 || repo.listFilter.SpaceID != sharedSpaceID {
		t.Fatalf("listed reminders=%#v filter=%#v, want all shared reminders in %q", got, repo.listFilter, sharedSpaceID)
	}
}

func TestListAuditsSanitizedSuccessMetadata(t *testing.T) {
	repo := &reminderRepositoryStub{}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_member", "user-1"),
	}}
	audit := &auditStoreStub{}
	service := newReminderService(repo, spaces, audit)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_member", "reminders:list")

	if _, err := service.List(context.Background(), actor, dto.ReminderListInput{}); err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %#v, want one success", audit.events)
	}
	event := audit.events[0]
	if event.Status != domainaudit.StatusSuccess || event.Action != "list" || event.Metadata["space_id"] != sharedSpaceID || event.Source != "mcp" || event.Channel != "whatsapp" || event.AgentProfile != "profile-1" {
		t.Fatalf("unexpected list audit: %#v", event)
	}
}

func TestCompleteOwnerCanCompleteAnyReminderInSpace(t *testing.T) {
	reminder := &domainreminder.Reminder{
		ID:                reminderID,
		SpaceID:           sharedSpaceID,
		CreatedByMemberID: creatorID,
		AssigneeMemberID:  stringPtr(assigneeID),
		Status:            domainreminder.StatusPending,
	}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
		membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_owner", "reminders:update")

	got, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID)
	if err != nil {
		t.Fatalf("complete reminder: %v", err)
	}
	if got.Status != domainreminder.StatusCompleted || !repo.completed {
		t.Fatalf("completion = %#v, repository completed=%v", got, repo.completed)
	}
}

func TestCreateAuditsSanitizedSpaceAndExternalMetadata(t *testing.T) {
	repo := &reminderRepositoryStub{}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(personalSpaceID, creatorID, "space_owner", "user-1"),
	}}
	audit := &auditStoreStub{}
	service := newReminderService(repo, spaces, audit)
	actor := actor(personalSpaceID, domainspace.TypePersonal, creatorID, "space_owner", "reminders:create")

	if _, err := service.Create(context.Background(), actor, dto.ReminderCreateInput{
		Title:       "Pay electricity bill",
		ScheduledAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %#v, want one success", audit.events)
	}
	event := audit.events[0]
	if event.Status != domainaudit.StatusSuccess || event.Action != domainaudit.ActionCreate || event.Resource != "reminder" {
		t.Fatalf("unexpected success audit: %#v", event)
	}
	if event.Metadata["space_id"] != personalSpaceID || event.Source != "mcp" || event.Channel != "whatsapp" || event.AgentProfile != "profile-1" {
		t.Fatalf("unsanitized audit metadata: %#v", event)
	}
}

func TestCreateAuditPreservesImpersonatorAndSubject(t *testing.T) {
	repo := &reminderRepositoryStub{}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(personalSpaceID, creatorID, "space_member", "subject-user"),
	}}
	audit := &auditStoreStub{}
	service := newReminderService(repo, spaces, audit)
	actor := actor(personalSpaceID, domainspace.TypePersonal, creatorID, "space_member", "reminders:create")
	actor.UserID = "subject-user"
	actor.InitiatorUserID = "operator-user"
	actor.InitiatorRoleName = "space_admin"

	if _, err := service.Create(context.Background(), actor, dto.ReminderCreateInput{
		Title:       "Pay electricity bill",
		ScheduledAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %#v, want one success", audit.events)
	}
	event := audit.events[0]
	if event.ActorUserID != "operator-user" || event.ActorRole != "space_admin" {
		t.Fatalf("audit initiator = user %q role %q, want operator-user/space_admin", event.ActorUserID, event.ActorRole)
	}
	if event.Metadata["subject_user_id"] != "subject-user" {
		t.Fatalf("subject metadata = %#v, want subject-user", event.Metadata)
	}
}

func TestCreateValidationFailureAuditsSanitizedFailureMetadata(t *testing.T) {
	repo := &reminderRepositoryStub{}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(personalSpaceID, creatorID, "space_owner", "user-1"),
	}}
	audit := &auditStoreStub{}
	service := newReminderService(repo, spaces, audit)
	actor := actor(personalSpaceID, domainspace.TypePersonal, creatorID, "space_owner", "reminders:create")

	if _, err := service.Create(context.Background(), actor, dto.ReminderCreateInput{ScheduledAt: time.Now().UTC()}); err == nil {
		t.Fatal("create with empty title unexpectedly succeeded")
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %#v, want one failure", audit.events)
	}
	event := audit.events[0]
	if event.Status != domainaudit.StatusFailed || event.ErrorMessage != "validation" || event.Metadata["space_id"] != personalSpaceID {
		t.Fatalf("unexpected failure audit: %#v", event)
	}
}

func TestCompleteAuditsSanitizedSuccessMetadata(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: assigneeID, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_admin", "user-1"),
	}}
	audit := &auditStoreStub{}
	service := newReminderService(repo, spaces, audit)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_admin", "reminders:update")

	if _, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID); err != nil {
		t.Fatalf("complete reminder: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %#v, want one success", audit.events)
	}
	event := audit.events[0]
	if event.Status != domainaudit.StatusSuccess || event.Action != domainaudit.ActionUpdate || event.Metadata["space_id"] != sharedSpaceID || event.Source != "mcp" || event.Channel != "whatsapp" || event.AgentProfile != "profile-1" {
		t.Fatalf("unexpected completion audit: %#v", event)
	}
}

func TestCompleteMemberCanCompleteAssignedReminder(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, AssigneeMemberID: stringPtr(assigneeID), Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, assigneeID, "space_member", "reminders:update")

	if _, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID); err != nil {
		t.Fatalf("assigned member complete: %v", err)
	}
}

func TestCompleteMemberCanCompleteOwnReminder(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: assigneeID, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, assigneeID, "space_member", "reminders:update")

	if _, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID); err != nil {
		t.Fatalf("own member complete: %v", err)
	}
}

func TestCompleteRejectsUnrelatedMemberAndViewer(t *testing.T) {
	tests := []struct {
		name  string
		actor domainidentity.ActorContext
	}{
		{
			name:  "unrelated member",
			actor: actor(sharedSpaceID, domainspace.TypeShared, "member-other", "space_member", "reminders:update"),
		},
		{
			name:  "viewer",
			actor: actor(sharedSpaceID, domainspace.TypeShared, "member-viewer", "space_viewer", "reminders:update"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, AssigneeMemberID: stringPtr(assigneeID), Status: domainreminder.StatusPending}
			repo := &reminderRepositoryStub{found: reminder}
			spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
				membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
				membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
				membership(sharedSpaceID, "member-other", "space_member", "user-3"),
				membership(sharedSpaceID, "member-viewer", "space_viewer", "user-4"),
			}}
			service := newReminderService(repo, spaces, nil)

			_, err := service.Complete(context.Background(), tt.actor, sharedSpaceID, reminderID)
			if !errors.Is(err, authorization.ErrForbidden) {
				t.Fatalf("complete error = %v, want forbidden", err)
			}
			if repo.completed {
				t.Fatal("forbidden member completed reminder")
			}
		})
	}
}

func TestCompleteRejectsInactiveActorAndCrossSpaceResourceAsNotFound(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, Status: domainreminder.StatusPending}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
		{ID: "member-inactive", SpaceID: sharedSpaceID, UserID: "user-9", RoleName: "space_member", Status: domainspace.StatusInactive},
	}}
	tests := []struct {
		name  string
		actor domainidentity.ActorContext
		space string
	}{
		{name: "inactive actor", actor: actor(sharedSpaceID, domainspace.TypeShared, "member-inactive", "space_member", "reminders:update"), space: sharedSpaceID},
		{name: "cross space", actor: actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_owner", "reminders:update"), space: "space-other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &reminderRepositoryStub{found: reminder}
			service := newReminderService(repo, spaces, nil)
			_, err := service.Complete(context.Background(), tt.actor, tt.space, reminderID)
			if !errors.Is(err, authorization.ErrNotFound) {
				t.Fatalf("complete error = %v, want not found", err)
			}
			if repo.completed {
				t.Fatal("not-found completion mutated reminder")
			}
		})
	}
}

func TestCompleteStatusRaceReturnsConflict(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder, completeErr: domainreminder.ErrStatusConflict}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_owner", "reminders:update")

	_, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("complete error = %v, want conflict", err)
	}
}

func TestUpdateOwnerCanPatchPendingReminder(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: assigneeID, Status: domainreminder.StatusPending, Title: "Old title"}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
		membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_owner", "reminders:update")

	title := "New title"
	got, err := service.Update(context.Background(), actor, sharedSpaceID, reminderID, dto.ReminderUpdateInput{Title: &title})
	if err != nil {
		t.Fatalf("update reminder: %v", err)
	}
	if !repo.updated || repo.updateSpace != sharedSpaceID || repo.updateID != reminderID || repo.updatePatch.Title == nil || *repo.updatePatch.Title != title {
		t.Fatalf("unexpected update: updated=%v space=%q id=%q patch=%#v", repo.updated, repo.updateSpace, repo.updateID, repo.updatePatch)
	}
	if got == nil || got.Title != title {
		t.Fatalf("updated result = %#v, want title %q", got, title)
	}
}

func TestUpdateMemberCannotMutateAnotherMembersReminder(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: assigneeID, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_member", "user-1"),
		membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_member", "reminders:update")
	title := "Nope"

	_, err := service.Update(context.Background(), actor, sharedSpaceID, reminderID, dto.ReminderUpdateInput{Title: &title})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("update error = %v, want forbidden", err)
	}
	if repo.updated {
		t.Fatal("unauthorized member updated reminder")
	}
}

func TestDeleteOwnerSoftDeletesPendingReminder(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: assigneeID, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_admin", "user-1"),
		membership(sharedSpaceID, assigneeID, "space_member", "user-2"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_admin", "reminders:delete")

	got, err := service.Delete(context.Background(), actor, sharedSpaceID, reminderID)
	if err != nil {
		t.Fatalf("delete reminder: %v", err)
	}
	if !repo.deleted || repo.deleteStatus != domainreminder.StatusCancelled || got == nil || got.Status != domainreminder.StatusCancelled {
		t.Fatalf("delete result=%#v repo=%#v", got, repo)
	}
}

func TestCompleteTerminalReminderReturnsConflict(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, Status: domainreminder.StatusCompleted}
	repo := &reminderRepositoryStub{found: reminder}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_owner", "reminders:update")

	_, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("complete error = %v, want conflict", err)
	}
	if repo.completed {
		t.Fatal("terminal reminder was updated")
	}
}

func TestCompletePropagatesReminderLookupError(t *testing.T) {
	lookupErr := errors.New("reminder store unavailable")
	repo := &reminderRepositoryStub{findErr: lookupErr}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, creatorID, "space_owner", "user-1"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, creatorID, "space_owner", "reminders:update")

	_, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("complete error = %v, want lookup error", err)
	}
}

func TestCompleteUnauthorizedMemberCannotLearnTerminalState(t *testing.T) {
	repo := &reminderRepositoryStub{found: &domainreminder.Reminder{
		ID: reminderID, SpaceID: sharedSpaceID, CreatedByMemberID: creatorID, Status: domainreminder.StatusCompleted,
	}}
	spaces := &spaceRepositoryStub{members: []domainspace.ResolvedMembership{
		membership(sharedSpaceID, "member-other", "space_member", "user-1"),
	}}
	service := newReminderService(repo, spaces, nil)
	actor := actor(sharedSpaceID, domainspace.TypeShared, "member-other", "space_member", "reminders:update")

	_, err := service.Complete(context.Background(), actor, sharedSpaceID, reminderID)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("complete error = %v, want forbidden", err)
	}
}

func stringPtr(value string) *string { return &value }
