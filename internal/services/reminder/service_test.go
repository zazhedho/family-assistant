package servicereminder

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	domainfamilymember "github.com/zazhedho/family-assistant/internal/domain/familymember"
	domainreminder "github.com/zazhedho/family-assistant/internal/domain/reminder"
	"github.com/zazhedho/family-assistant/internal/services/authorization"
	identity "github.com/zazhedho/family-assistant/internal/services/identity"
	"gorm.io/gorm"
)

const (
	childMemberUUID  = "00000000-0000-0000-0000-000000000001"
	parentMemberUUID = "00000000-0000-0000-0000-000000000002"
	otherParentUUID  = "00000000-0000-0000-0000-000000000003"
	targetMemberUUID = "00000000-0000-0000-0000-000000000004"
	reminderUUID     = "00000000-0000-0000-0000-000000000101"
)

type reminderRepositoryStub struct {
	created    *domainreminder.Reminder
	listed     []domainreminder.Reminder
	listFilter domainreminder.ListFilter
	found      *domainreminder.Reminder
	findErr    error
	findCalls  int
	findFamily string
	findID     string
	updated    *domainreminder.Reminder
	createErr  error
	listErr    error
	listCalls  int
	updateErr  error
}

func (s *reminderRepositoryStub) Create(_ context.Context, reminder *domainreminder.Reminder) error {
	if s.createErr != nil {
		return s.createErr
	}
	copy := *reminder
	s.created = &copy
	return nil
}

func (s *reminderRepositoryStub) FindByIDInFamily(_ context.Context, familyID, reminderID string) (*domainreminder.Reminder, error) {
	s.findCalls++
	s.findFamily, s.findID = familyID, reminderID
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
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	s.listFilter = filter
	return append([]domainreminder.Reminder(nil), s.listed...), nil
}

func (s *reminderRepositoryStub) Update(_ context.Context, reminder *domainreminder.Reminder) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	copy := *reminder
	s.updated = &copy
	return nil
}

type familyMemberRepositoryStub struct {
	byID       map[string]*domainfamilymember.FamilyMember
	findErr    error
	findCalls  int
	lastFamily string
	lastID     string
}

func (s *familyMemberRepositoryStub) FindActiveByHermesProfile(context.Context, string) (*domainfamilymember.ResolvedMember, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *familyMemberRepositoryStub) FindActiveByUserID(context.Context, string) (*domainfamilymember.ResolvedMember, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *familyMemberRepositoryStub) FindActiveByID(_ context.Context, familyID, memberID string) (*domainfamilymember.FamilyMember, error) {
	s.findCalls++
	s.lastFamily, s.lastID = familyID, memberID
	if s.findErr != nil {
		return nil, s.findErr
	}
	member, ok := s.byID[memberID]
	if !ok || member.FamilyID != familyID {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *member
	return &copy, nil
}

type authorizationCall struct {
	permission string
	resource   authorization.Resource
}

type authorizerStub struct {
	calls []authorizationCall
	err   error
}

func (s *authorizerStub) Authorize(_ context.Context, _ identity.ActorContext, permission string, resource authorization.Resource) error {
	s.calls = append(s.calls, authorizationCall{permission: permission, resource: resource})
	return s.err
}

type auditServiceStub struct {
	events []domainaudit.AuditEvent
}

func (s *auditServiceStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func reminderActor(role, member, family string, permissions ...string) identity.ActorContext {
	permissionSet := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		permissionSet[permission] = struct{}{}
	}
	return identity.ActorContext{
		UserID:          "user-" + member,
		MemberID:        member,
		FamilyID:        family,
		RoleName:        role,
		Permissions:     permissionSet,
		Channel:         "whatsapp",
		HermesProfileID: "profile-" + member,
	}
}

func member(id, family, role string) *domainfamilymember.FamilyMember {
	return &domainfamilymember.FamilyMember{ID: id, FamilyID: family, RoleName: role}
}

func validCreateInput() CreateInput {
	return CreateInput{
		Title:       "Pay electricity bill",
		Description: "Before Friday",
		ScheduledAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
		Scope:       domainreminder.ScopePersonal,
	}
}

func newReminderService(repo *reminderRepositoryStub, members *familyMemberRepositoryStub, authz authorization.Authorizer, auditService auditStore) Service {
	return NewReminderService(repo, members, authz, auditService)
}

func TestCreateOwnPersonalReminderDerivesTrustedOwnershipAndAudits(t *testing.T) {
	repo := &reminderRepositoryStub{}
	auditService := &auditServiceStub{}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:create")
	service := newReminderService(repo, &familyMemberRepositoryStub{}, authorization.NewAuthorizer(), auditService)

	got, err := service.Create(context.Background(), actor, validCreateInput())
	if err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if got == nil || repo.created == nil {
		t.Fatal("expected created reminder")
	}
	if repo.created.FamilyID != actor.FamilyID || repo.created.OwnerMemberID != actor.MemberID || repo.created.CreatedByMemberID != actor.MemberID {
		t.Fatalf("expected trusted ownership fields, got %#v", repo.created)
	}
	if repo.created.Scope != domainreminder.ScopePersonal || repo.created.Status != domainreminder.StatusPending {
		t.Fatalf("unexpected defaults: %#v", repo.created)
	}
	if len(auditService.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(auditService.events))
	}
	event := auditService.events[0]
	if event.ActorUserID != actor.UserID || event.Resource != "reminder" || event.ResourceID != got.ID || event.Action != domainaudit.ActionCreate || event.Status != domainaudit.StatusSuccess {
		t.Fatalf("unexpected create audit event: %#v", event)
	}
	for key, want := range map[string]string{
		"actor_member_id":          actor.MemberID,
		"resource_owner_member_id": actor.MemberID,
		"source":                   "mcp",
		"channel":                  actor.Channel,
		"agent_profile":            actor.HermesProfileID,
	} {
		if got := event.Metadata[key]; got != want {
			t.Errorf("metadata[%q] = %v, want %q", key, got, want)
		}
	}
}

func TestCreateAuditUsesImpersonatorAsInitiatorAndPreservesSubject(t *testing.T) {
	repo := &reminderRepositoryStub{}
	auditService := &auditServiceStub{}
	actor := reminderActor("parent", "member-effective", "family-1", "reminders:create")
	actor.InitiatorUserID = "user-operator"
	actor.InitiatorRoleName = "admin"
	service := newReminderService(repo, &familyMemberRepositoryStub{}, authorization.NewAuthorizer(), auditService)

	if _, err := service.Create(context.Background(), actor, validCreateInput()); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if len(auditService.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(auditService.events))
	}
	event := auditService.events[0]
	if event.ActorUserID != "user-operator" || event.ActorRole != "admin" {
		t.Fatalf("audit initiator = %q/%q, want operator/admin", event.ActorUserID, event.ActorRole)
	}
	if got := event.Metadata["subject_user_id"]; got != actor.UserID {
		t.Fatalf("subject_user_id = %v, want %q", got, actor.UserID)
	}
}

func TestCreateDefaultsBlankScopeToPersonal(t *testing.T) {
	repo := &reminderRepositoryStub{}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:create")
	service := newReminderService(repo, &familyMemberRepositoryStub{}, authorization.NewAuthorizer(), nil)

	input := validCreateInput()
	input.Scope = ""
	if _, err := service.Create(context.Background(), actor, input); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if repo.created.Scope != domainreminder.ScopePersonal {
		t.Fatalf("scope = %q, want PERSONAL", repo.created.Scope)
	}
}

func TestCreateParentCanDelegateToChildInSameFamily(t *testing.T) {
	repo := &reminderRepositoryStub{}
	members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
		childMemberUUID: member(childMemberUUID, "family-1", "child"),
	}}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:create")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)
	input := validCreateInput()
	input.TargetMemberID = stringPtr(childMemberUUID)

	if _, err := service.Create(context.Background(), actor, input); err != nil {
		t.Fatalf("create delegated reminder: %v", err)
	}
	if repo.created.OwnerMemberID != childMemberUUID || members.lastFamily != actor.FamilyID || members.lastID != childMemberUUID {
		t.Fatalf("unexpected delegated ownership/lookup: %#v, %q/%q", repo.created, members.lastFamily, members.lastID)
	}
}

func TestCreateFamilyScopeKeepsActorOwnershipWithTargetSelector(t *testing.T) {
	repo := &reminderRepositoryStub{}
	members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
		childMemberUUID: member(childMemberUUID, "family-1", "child"),
	}}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:create")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)
	input := validCreateInput()
	input.Scope = domainreminder.ScopeFamily
	input.TargetMemberID = stringPtr(" " + childMemberUUID + " ")

	if _, err := service.Create(context.Background(), actor, input); err != nil {
		t.Fatalf("create family reminder: %v", err)
	}
	if repo.created.OwnerMemberID != actor.MemberID || repo.created.CreatedByMemberID != actor.MemberID || repo.created.FamilyID != actor.FamilyID {
		t.Fatalf("family reminder attribution = %#v, want actor ownership", repo.created)
	}
}

func TestCreateRejectsUnauthorizedDelegationAndCrossFamilyTarget(t *testing.T) {
	tests := []struct {
		name      string
		actor     identity.ActorContext
		target    *domainfamilymember.FamilyMember
		findErr   error
		wantError error
	}{
		{
			name:      "parent to other parent",
			actor:     reminderActor("parent", "parent-1", "family-1", "reminders:create"),
			target:    member(otherParentUUID, "family-1", "parent"),
			wantError: authorization.ErrForbidden,
		},
		{
			name:      "child to parent",
			actor:     reminderActor("child", "child-1", "family-1", "reminders:create"),
			target:    member(parentMemberUUID, "family-1", "parent"),
			wantError: authorization.ErrForbidden,
		},
		{
			name:      "cross family target",
			actor:     reminderActor("parent", "parent-1", "family-1", "reminders:create"),
			findErr:   gorm.ErrRecordNotFound,
			wantError: authorization.ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &reminderRepositoryStub{}
			members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{}}
			if tt.target != nil {
				members.byID[tt.target.ID] = tt.target
			}
			members.findErr = tt.findErr
			service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)
			input := validCreateInput()
			input.TargetMemberID = stringPtr(targetMemberUUID)
			if tt.target != nil {
				input.TargetMemberID = &tt.target.ID
			}

			_, err := service.Create(context.Background(), tt.actor, input)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("create error = %v, want %v", err, tt.wantError)
			}
			if repo.created != nil {
				t.Fatal("unexpected persistence after rejected create")
			}
		})
	}
}

func TestCreateValidatesTitleAndScheduleBeforePersistence(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input CreateInput
		field string
	}{
		{name: "blank title", input: CreateInput{ScheduledAt: time.Now()}, field: "title"},
		{name: "zero scheduled time", input: CreateInput{Title: "Reminder"}, field: "scheduled_at"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &reminderRepositoryStub{}
			actor := reminderActor("parent", "parent-1", "family-1", "reminders:create")
			service := newReminderService(repo, &familyMemberRepositoryStub{}, authorization.NewAuthorizer(), nil)
			_, err := service.Create(context.Background(), actor, tt.input)
			var validationErr *authorization.ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.field {
				t.Fatalf("error = %T %v, want validation field %q", err, err, tt.field)
			}
			if repo.created != nil {
				t.Fatal("unexpected persistence after validation failure")
			}
		})
	}
}

func TestListUsesRestrictiveOwnerOrFamilyFilters(t *testing.T) {
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:list")
	tests := []struct {
		name       string
		input      ListInput
		target     *domainfamilymember.FamilyMember
		wantOwner  string
		wantScope  domainreminder.Scope
		wantFamily string
	}{
		{name: "nil scope own personal", wantOwner: "parent-1", wantScope: domainreminder.ScopePersonal, wantFamily: "family-1"},
		{name: "child personal", input: ListInput{Scope: scopePtr(domainreminder.ScopePersonal), TargetMemberID: stringPtr(childMemberUUID)}, target: member(childMemberUUID, "family-1", "child"), wantOwner: childMemberUUID, wantScope: domainreminder.ScopePersonal, wantFamily: "family-1"},
		{name: "family", input: ListInput{Scope: scopePtr(domainreminder.ScopeFamily)}, wantScope: domainreminder.ScopeFamily, wantFamily: "family-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &reminderRepositoryStub{listed: []domainreminder.Reminder{{ID: "r-1"}}}
			members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{}}
			if tt.target != nil {
				members.byID[tt.target.ID] = tt.target
			}
			service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)
			got, err := service.List(context.Background(), actor, tt.input)
			if err != nil {
				t.Fatalf("list reminders: %v", err)
			}
			if len(got) != 1 || repo.listFilter.FamilyID != tt.wantFamily || repo.listFilter.Scope == nil || *repo.listFilter.Scope != tt.wantScope {
				t.Fatalf("unexpected list result/filter: %#v / %#v", got, repo.listFilter)
			}
			if tt.wantOwner == "" {
				if repo.listFilter.OwnerMemberID != nil {
					t.Fatalf("family list unexpectedly owner-scoped: %#v", repo.listFilter)
				}
			} else if repo.listFilter.OwnerMemberID == nil || *repo.listFilter.OwnerMemberID != tt.wantOwner {
				t.Fatalf("owner filter = %#v, want %q", repo.listFilter.OwnerMemberID, tt.wantOwner)
			}
		})
	}
}

func TestListRejectsUnauthorizedOrCrossFamilyTarget(t *testing.T) {
	actor := reminderActor("child", "child-1", "family-1", "reminders:list")
	for _, tt := range []struct {
		name    string
		target  *domainfamilymember.FamilyMember
		findErr error
		wantErr error
	}{
		{name: "parent target", target: member(parentMemberUUID, "family-1", "parent"), wantErr: authorization.ErrForbidden},
		{name: "cross family target", findErr: gorm.ErrRecordNotFound, wantErr: authorization.ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{}, findErr: tt.findErr}
			targetID := targetMemberUUID
			if tt.target != nil {
				members.byID[tt.target.ID] = tt.target
				targetID = tt.target.ID
			}
			service := newReminderService(&reminderRepositoryStub{}, members, authorization.NewAuthorizer(), nil)
			_, err := service.List(context.Background(), actor, ListInput{Scope: scopePtr(domainreminder.ScopePersonal), TargetMemberID: &targetID})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("list error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceRejectsInvalidIDsBeforeRepositoryCalls(t *testing.T) {
	t.Run("create target", func(t *testing.T) {
		repo := &reminderRepositoryStub{}
		members := &familyMemberRepositoryStub{}
		actor := reminderActor("parent", "parent-1", "family-1", "reminders:create")
		service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)
		input := validCreateInput()
		input.TargetMemberID = stringPtr("not-a-uuid")

		_, err := service.Create(context.Background(), actor, input)
		assertValidationField(t, err, "target_member_id")
		if members.findCalls != 0 || repo.created != nil {
			t.Fatalf("invalid create target reached repository: member calls=%d reminder=%#v", members.findCalls, repo.created)
		}
	})

	t.Run("list target", func(t *testing.T) {
		repo := &reminderRepositoryStub{}
		members := &familyMemberRepositoryStub{}
		actor := reminderActor("parent", "parent-1", "family-1", "reminders:list")
		service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)
		input := ListInput{TargetMemberID: stringPtr("not-a-uuid")}

		_, err := service.List(context.Background(), actor, input)
		assertValidationField(t, err, "target_member_id")
		if members.findCalls != 0 || repo.listCalls != 0 {
			t.Fatalf("invalid list target reached repository: member calls=%d list calls=%d", members.findCalls, repo.listCalls)
		}
	})

	t.Run("complete reminder", func(t *testing.T) {
		repo := &reminderRepositoryStub{}
		members := &familyMemberRepositoryStub{}
		actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
		service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)

		_, err := service.Complete(context.Background(), actor, "not-a-uuid")
		assertValidationField(t, err, "reminder_id")
		if repo.findCalls != 0 || members.findCalls != 0 {
			t.Fatalf("invalid reminder ID reached repository: reminder calls=%d member calls=%d", repo.findCalls, members.findCalls)
		}
	})
}

func TestCompleteTrimsReminderIDBeforeRepositoryLookup(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
		"parent-1": member("parent-1", "family-1", "parent"),
	}}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)

	if _, err := service.Complete(context.Background(), actor, " "+reminderUUID+" "); err != nil {
		t.Fatalf("complete trimmed reminder ID: %v", err)
	}
	if repo.findID != reminderUUID {
		t.Fatalf("repository reminder ID = %q, want %q", repo.findID, reminderUUID)
	}
}

func TestListRejectsInvalidStatusAndInvertedDateRangeBeforeQuery(t *testing.T) {
	tests := []struct {
		name  string
		input ListInput
		field string
	}{
		{
			name:  "invalid status",
			input: ListInput{Status: statusPtr(domainreminder.Status("UNKNOWN"))},
			field: "status",
		},
		{
			name:  "from after to",
			input: ListInput{From: timePtr(time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)), To: timePtr(time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))},
			field: "from",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &reminderRepositoryStub{}
			actor := reminderActor("parent", "parent-1", "family-1", "reminders:list")
			service := newReminderService(repo, &familyMemberRepositoryStub{}, authorization.NewAuthorizer(), nil)

			_, err := service.List(context.Background(), actor, tt.input)
			assertValidationField(t, err, tt.field)
			if repo.listCalls != 0 {
				t.Fatalf("invalid list input reached repository: %d calls", repo.listCalls)
			}
		})
	}
}

func TestCompleteFamilyScopeDoesNotResolveOwnerMembership(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "inactive-owner", Scope: domainreminder.ScopeFamily, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	members := &familyMemberRepositoryStub{findErr: errors.New("family scope must not resolve owner")}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)

	got, err := service.Complete(context.Background(), actor, reminderUUID)
	if err != nil {
		t.Fatalf("complete family reminder: %v", err)
	}
	if got.Status != domainreminder.StatusCompleted || repo.updated == nil || members.findCalls != 0 {
		t.Fatalf("family completion resolved owner or failed: got=%#v updated=%#v member calls=%d", got, repo.updated, members.findCalls)
	}
}

func TestCompleteUpdateConflictDoesNotAuditSuccess(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder, updateErr: domainreminder.ErrStatusConflict}
	members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
		"parent-1": member("parent-1", "family-1", "parent"),
	}}
	auditService := &auditServiceStub{}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), auditService)

	_, err := service.Complete(context.Background(), actor, reminderUUID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("complete error = %v, want ErrConflict", err)
	}
	if len(auditService.events) != 0 {
		t.Fatalf("conflicted completion emitted audit success: %#v", auditService.events)
	}
}

func TestCompleteAuthorizesOwnerAndPersistsCompletionWithAudit(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "child-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
		"child-1": member("child-1", "family-1", "child"),
	}}
	auditService := &auditServiceStub{}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), auditService)

	got, err := service.Complete(context.Background(), actor, reminder.ID)
	if err != nil {
		t.Fatalf("complete reminder: %v", err)
	}
	if got.Status != domainreminder.StatusCompleted || got.CompletedAt == nil || repo.updated == nil || repo.updated.CompletedAt == nil {
		t.Fatalf("unexpected completion: %#v / %#v", got, repo.updated)
	}
	if len(auditService.events) != 1 {
		t.Fatalf("expected completion audit, got %d", len(auditService.events))
	}
	event := auditService.events[0]
	if event.ActorUserID != actor.UserID || event.Action != domainaudit.ActionUpdate || event.Status != domainaudit.StatusSuccess || event.ResourceID != reminder.ID {
		t.Fatalf("unexpected completion audit: %#v", event)
	}
	for key, want := range map[string]string{
		"actor_member_id":          actor.MemberID,
		"resource_owner_member_id": "child-1",
		"source":                   "mcp",
		"channel":                  actor.Channel,
		"agent_profile":            actor.HermesProfileID,
		"status":                   string(domainreminder.StatusCompleted),
	} {
		if got := event.Metadata[key]; got != want {
			t.Errorf("metadata[%q] = %v, want %q", key, got, want)
		}
	}
}

func TestCompleteOwnerPendingReminderSucceeds(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder}
	members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
		"parent-1": member("parent-1", "family-1", "parent"),
	}}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)

	got, err := service.Complete(context.Background(), actor, reminder.ID)
	if err != nil {
		t.Fatalf("complete own reminder: %v", err)
	}
	if got.Status != domainreminder.StatusCompleted || got.CompletedAt == nil || repo.updated == nil {
		t.Fatalf("unexpected own completion: %#v / %#v", got, repo.updated)
	}
}

func TestCompleteRejectsUnauthorizedTerminalAndCrossFamilyReminders(t *testing.T) {
	tests := []struct {
		name     string
		actor    identity.ActorContext
		reminder *domainreminder.Reminder
		findErr  error
		wantErr  error
	}{
		{
			name:     "other parent",
			actor:    reminderActor("parent", "parent-2", "family-1", "reminders:update"),
			reminder: &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending},
			wantErr:  authorization.ErrForbidden,
		},
		{
			name:     "child to parent",
			actor:    reminderActor("child", "child-1", "family-1", "reminders:update"),
			reminder: &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending},
			wantErr:  authorization.ErrForbidden,
		},
		{
			name:     "already completed",
			actor:    reminderActor("parent", "parent-1", "family-1", "reminders:update"),
			reminder: &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusCompleted},
			wantErr:  ErrConflict,
		},
		{
			name:     "cancelled",
			actor:    reminderActor("parent", "parent-1", "family-1", "reminders:update"),
			reminder: &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusCancelled},
			wantErr:  ErrConflict,
		},
		{
			name:    "cross family id",
			actor:   reminderActor("parent", "parent-1", "family-1", "reminders:update"),
			findErr: gorm.ErrRecordNotFound,
			wantErr: authorization.ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &reminderRepositoryStub{found: tt.reminder, findErr: tt.findErr}
			members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
				"parent-1": member("parent-1", "family-1", "parent"),
			}}
			if tt.reminder != nil {
				members.byID[tt.reminder.OwnerMemberID] = member(tt.reminder.OwnerMemberID, "family-1", "parent")
			}
			service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)
			_, err := service.Complete(context.Background(), tt.actor, reminderUUID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("complete error = %v, want %v", err, tt.wantErr)
			}
			if repo.updated != nil {
				t.Fatal("unexpected update after rejected completion")
			}
		})
	}
}

func TestCompleteMapsMissingOwnerToNotFound(t *testing.T) {
	repo := &reminderRepositoryStub{found: &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "member-missing", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending}}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
	service := newReminderService(repo, &familyMemberRepositoryStub{}, authorization.NewAuthorizer(), nil)

	_, err := service.Complete(context.Background(), actor, reminderUUID)
	if !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("complete error = %v, want not found", err)
	}
}

func TestCompleteMapsUpdateNotFoundToAuthorizationNotFound(t *testing.T) {
	reminder := &domainreminder.Reminder{ID: reminderUUID, FamilyID: "family-1", OwnerMemberID: "parent-1", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending}
	repo := &reminderRepositoryStub{found: reminder, updateErr: gorm.ErrRecordNotFound}
	members := &familyMemberRepositoryStub{byID: map[string]*domainfamilymember.FamilyMember{
		"parent-1": member("parent-1", "family-1", "parent"),
	}}
	actor := reminderActor("parent", "parent-1", "family-1", "reminders:update")
	service := newReminderService(repo, members, authorization.NewAuthorizer(), nil)

	_, err := service.Complete(context.Background(), actor, reminder.ID)
	if !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("complete error = %v, want authorization not found", err)
	}
}

func stringPtr(value string) *string { return &value }

func scopePtr(value domainreminder.Scope) *domainreminder.Scope { return &value }

func statusPtr(value domainreminder.Status) *domainreminder.Status { return &value }

func timePtr(value time.Time) *time.Time { return &value }

func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()
	var validationErr *authorization.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != field {
		t.Fatalf("error = %T %v, want validation field %q", err, err, field)
	}
}
