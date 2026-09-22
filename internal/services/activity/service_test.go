package serviceactivity

import (
	"context"
	"errors"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	"family-assistant/internal/services/authorization"
	"gorm.io/gorm"
)

type activityRepositoryStub struct {
	created  *domainactivity.Activity
	listed   []domainactivity.Activity
	filter   domainactivity.ListFilter
	found    *domainactivity.Activity
	updated  bool
	deleted  bool
	update   domainactivity.UpdateFields
	updateID string
	deleteID string
}

func (s *activityRepositoryStub) Create(_ context.Context, activity *domainactivity.Activity) error {
	snapshot := *activity
	s.created = &snapshot
	return nil
}

func (s *activityRepositoryStub) List(_ context.Context, filter domainactivity.ListFilter) ([]domainactivity.Activity, error) {
	s.filter = filter
	return append([]domainactivity.Activity(nil), s.listed...), nil
}

func (s *activityRepositoryStub) FindByIDInSpace(_ context.Context, _, _ string) (*domainactivity.Activity, error) {
	if s.found == nil {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *s.found
	return &copy, nil
}

func (s *activityRepositoryStub) Update(_ context.Context, _, activityID string, fields domainactivity.UpdateFields, _ time.Time) error {
	s.updated, s.updateID, s.update = true, activityID, fields
	return nil
}

func (s *activityRepositoryStub) SoftDelete(_ context.Context, _, activityID string, _ time.Time) error {
	s.deleted, s.deleteID = true, activityID
	return nil
}

type activitySpaceRepositoryStub struct {
	members []domainspace.ResolvedMembership
}

func (s *activitySpaceRepositoryStub) CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error {
	return nil
}
func (s *activitySpaceRepositoryStub) ListActiveByUserID(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
}
func (s *activitySpaceRepositoryStub) FindActiveMembership(context.Context, string, string) (*domainspace.ResolvedMembership, error) {
	return nil, nil
}
func (s *activitySpaceRepositoryStub) ListActiveMembers(_ context.Context, spaceID string) ([]domainspace.ResolvedMembership, error) {
	result := make([]domainspace.ResolvedMembership, 0, len(s.members))
	for _, member := range s.members {
		if member.SpaceID == spaceID && member.Status == domainspace.StatusActive {
			result = append(result, member)
		}
	}
	return result, nil
}

func (s *activitySpaceRepositoryStub) Update(context.Context, string, domainspace.SpaceUpdateFields, time.Time) error {
	return nil
}
func (s *activitySpaceRepositoryStub) Archive(context.Context, string, time.Time) error {
	return nil
}
func (s *activitySpaceRepositoryStub) UpdateMemberRole(context.Context, string, string, string, time.Time) error {
	return nil
}
func (s *activitySpaceRepositoryStub) RemoveMember(context.Context, string, string, time.Time) error {
	return nil
}
func (s *activitySpaceRepositoryStub) CountActiveOwners(context.Context, string) (int64, error) {
	return 1, nil
}

type activityAuditStub struct {
	events []domainaudit.AuditEvent
}

func (s *activityAuditStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func activityActor(spaceID, memberID string, permissions ...string) domainidentity.ActorContext {
	permissionSet := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		permissionSet[permission] = struct{}{}
	}
	return domainidentity.ActorContext{
		UserID: "user-1", SpaceID: spaceID, MemberID: memberID, SpaceType: domainspace.TypeShared,
		Permissions: permissionSet, Source: "mcp", Channel: "whatsapp", ExternalID: "profile-1",
	}
}

func activityService(repo *activityRepositoryStub, spaces *activitySpaceRepositoryStub, audit *activityAuditStub) interfaceactivity.ServiceActivityInterface {
	return NewService(repo, spaces, authorization.NewAuthorizer(), audit)
}

func TestCreateActivityTrimsAndPersistsActorScope(t *testing.T) {
	repo := &activityRepositoryStub{}
	spaces := &activitySpaceRepositoryStub{members: []domainspace.ResolvedMembership{{ID: "member-1", SpaceID: "space-1", UserID: "user-1", Status: domainspace.StatusActive}}}
	service := activityService(repo, spaces, &activityAuditStub{})
	occurredAt := time.Date(2026, 9, 22, 8, 30, 0, 0, time.UTC)

	created, err := service.Create(context.Background(), activityActor("space-1", "member-1", "activities:create"), dto.ActivityCreateInput{
		Space: " space-1 ", Kind: " breastfeeding ", Note: " 10 minutes left side ", OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("create activity: %v", err)
	}
	if created == nil || repo.created == nil || repo.created.SpaceID != "space-1" || repo.created.CreatedByMemberID != "member-1" || repo.created.Kind != "breastfeeding" || repo.created.Note != "10 minutes left side" || !repo.created.OccurredAt.Equal(occurredAt) {
		t.Fatalf("created activity = %+v", repo.created)
	}
}

func TestListActivityScopesFilterAndRequiresMembership(t *testing.T) {
	from := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	repo := &activityRepositoryStub{listed: []domainactivity.Activity{{ID: "activity-1", SpaceID: "space-1", Kind: "diaper"}}}
	spaces := &activitySpaceRepositoryStub{members: []domainspace.ResolvedMembership{{ID: "member-1", SpaceID: "space-1", UserID: "user-1", Status: domainspace.StatusActive}}}
	service := activityService(repo, spaces, &activityAuditStub{})

	got, err := service.List(context.Background(), activityActor("space-1", "member-1", "activities:list"), dto.ActivityListInput{Space: "space-1", Kind: " diaper ", From: &from, To: &to, Limit: 20})
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if len(got) != 1 || repo.filter.SpaceID != "space-1" || repo.filter.Kind != "diaper" || repo.filter.From == nil || repo.filter.To == nil || repo.filter.Limit != 20 {
		t.Fatalf("list filter/result = %+v/%+v", repo.filter, got)
	}
}

func TestCreateActivityRequiresOccurredAt(t *testing.T) {
	repo := &activityRepositoryStub{}
	spaces := &activitySpaceRepositoryStub{members: []domainspace.ResolvedMembership{{ID: "member-1", SpaceID: "space-1", UserID: "user-1", Status: domainspace.StatusActive}}}
	service := activityService(repo, spaces, &activityAuditStub{})

	_, err := service.Create(context.Background(), activityActor("space-1", "member-1", "activities:create"), dto.ActivityCreateInput{Kind: "note", Note: "missing time"})
	if err == nil {
		t.Fatal("missing occurred_at was accepted")
	}
	if repo.created != nil {
		t.Fatal("invalid activity was persisted")
	}
}

func TestUpdateActivityOwnerCanPatchOwnActivity(t *testing.T) {
	activity := &domainactivity.Activity{ID: "activity-1", SpaceID: "space-1", CreatedByMemberID: "member-1", Kind: "note", Note: "old"}
	repo := &activityRepositoryStub{found: activity}
	spaces := &activitySpaceRepositoryStub{members: []domainspace.ResolvedMembership{{ID: "member-1", SpaceID: "space-1", UserID: "user-1", RoleName: "space_owner", Status: domainspace.StatusActive}}}
	service := activityService(repo, spaces, &activityAuditStub{})
	actor := activityActor("space-1", "member-1", "activities:update")
	actor.RoleName = "space_owner"
	note := "new"

	got, err := service.Update(context.Background(), actor, "space-1", "activity-1", dto.ActivityUpdateInput{Note: &note})
	if err != nil {
		t.Fatalf("update activity: %v", err)
	}
	if !repo.updated || repo.updateID != "activity-1" || repo.update.Note == nil || *repo.update.Note != note || got == nil || got.Note != note {
		t.Fatalf("update=%#v result=%#v", repo, got)
	}
}

func TestUpdateActivityMemberCannotMutateAnotherMemberActivity(t *testing.T) {
	repo := &activityRepositoryStub{found: &domainactivity.Activity{ID: "activity-1", SpaceID: "space-1", CreatedByMemberID: "member-2", Kind: "note", Note: "old"}}
	spaces := &activitySpaceRepositoryStub{members: []domainspace.ResolvedMembership{
		{ID: "member-1", SpaceID: "space-1", UserID: "user-1", RoleName: "space_member", Status: domainspace.StatusActive},
		{ID: "member-2", SpaceID: "space-1", UserID: "user-2", RoleName: "space_member", Status: domainspace.StatusActive},
	}}
	service := activityService(repo, spaces, &activityAuditStub{})
	actor := activityActor("space-1", "member-1", "activities:update")
	actor.RoleName = "space_member"
	note := "nope"

	_, err := service.Update(context.Background(), actor, "space-1", "activity-1", dto.ActivityUpdateInput{Note: &note})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
	if repo.updated {
		t.Fatal("unauthorized member updated activity")
	}
}

func TestDeleteActivityOwnerSoftDeletesActivity(t *testing.T) {
	repo := &activityRepositoryStub{found: &domainactivity.Activity{ID: "activity-1", SpaceID: "space-1", CreatedByMemberID: "member-1", Kind: "note", Note: "old"}}
	spaces := &activitySpaceRepositoryStub{members: []domainspace.ResolvedMembership{{ID: "member-1", SpaceID: "space-1", UserID: "user-1", RoleName: "space_admin", Status: domainspace.StatusActive}}}
	service := activityService(repo, spaces, &activityAuditStub{})
	actor := activityActor("space-1", "member-1", "activities:delete")
	actor.RoleName = "space_admin"

	got, err := service.Delete(context.Background(), actor, "space-1", "activity-1")
	if err != nil {
		t.Fatalf("delete activity: %v", err)
	}
	if !repo.deleted || repo.deleteID != "activity-1" || got == nil || !got.DeletedAt.Valid {
		t.Fatalf("delete=%#v result=%#v", repo, got)
	}
}

var _ interfaceactivity.RepoActivityInterface = (*activityRepositoryStub)(nil)
