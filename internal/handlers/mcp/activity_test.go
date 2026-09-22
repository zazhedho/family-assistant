package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	serviceidentity "family-assistant/internal/services/identity"
)

type mcpActivityServiceStub struct {
	created     *domainactivity.Activity
	createErr   error
	createActor domainidentity.ActorContext
	createInput dto.ActivityCreateInput
	createCalls int
	listed      []domainactivity.Activity
	listErr     error
	listActor   domainidentity.ActorContext
	listInput   dto.ActivityListInput
	listCalls   int
	updateActor domainidentity.ActorContext
	updateInput dto.ActivityUpdateInput
	updateSpace string
	updateID    string
	updateCalls int
	updateErr   error
	deleteActor domainidentity.ActorContext
	deleteSpace string
	deleteID    string
	deleteCalls int
	deleteErr   error
}

func (s *mcpActivityServiceStub) Create(_ context.Context, actor domainidentity.ActorContext, input dto.ActivityCreateInput) (*domainactivity.Activity, error) {
	s.createCalls++
	s.createActor, s.createInput = actor, input
	return s.created, s.createErr
}

func (s *mcpActivityServiceStub) List(_ context.Context, actor domainidentity.ActorContext, input dto.ActivityListInput) ([]domainactivity.Activity, error) {
	s.listCalls++
	s.listActor, s.listInput = actor, input
	return s.listed, s.listErr
}

func (s *mcpActivityServiceStub) Update(_ context.Context, actor domainidentity.ActorContext, spaceID, activityID string, input dto.ActivityUpdateInput) (*domainactivity.Activity, error) {
	s.updateCalls++
	s.updateActor, s.updateInput, s.updateSpace, s.updateID = actor, input, spaceID, activityID
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return &domainactivity.Activity{ID: activityID, SpaceID: spaceID, Kind: "note", Note: "updated"}, nil
}

func (s *mcpActivityServiceStub) Delete(_ context.Context, actor domainidentity.ActorContext, spaceID, activityID string) (*domainactivity.Activity, error) {
	s.deleteCalls++
	s.deleteActor, s.deleteSpace, s.deleteID = actor, spaceID, activityID
	if s.deleteErr != nil {
		return nil, s.deleteErr
	}
	return &domainactivity.Activity{ID: activityID, SpaceID: spaceID}, nil
}

func TestActivityCreateSelectsSpaceAndNormalizesTime(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000501"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", spaceID, "Baby", domainspace.TypeShared, "role-member"),
		}},
		permissions: []domainpermission.Permission{{Resource: "activities", Action: "create"}},
	}
	service := &mcpActivityServiceStub{created: &domainactivity.Activity{ID: "activity-1", SpaceID: spaceID, Kind: "breastfeeding"}}

	got, err := ActivityCreate(mcpExternalContext(), resolver, service, ActivityCreateInput{
		Space: " baby ", Kind: " breastfeeding ", Note: "10 minutes", OccurredAt: "2026-09-22T08:30:00+07:00",
	})
	if err != nil {
		t.Fatalf("activity_create: %v", err)
	}
	if got == nil || got.ID != "activity-1" || service.createCalls != 1 || service.createActor.SpaceID != spaceID || service.createInput.Space != spaceID || service.createInput.Kind != " breastfeeding " {
		t.Fatalf("output/service args = %+v/%+v", got, service)
	}
	want := time.Date(2026, 9, 22, 1, 30, 0, 0, time.UTC)
	if !service.createInput.OccurredAt.Equal(want) {
		t.Fatalf("occurred_at = %s, want %s", service.createInput.OccurredAt, want)
	}
}

func TestActivityListPassesSpaceKindAndDateRange(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000501"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", spaceID, "Baby", domainspace.TypeShared, "role-member"),
		}},
		permissions: []domainpermission.Permission{{Resource: "activities", Action: "list"}},
	}
	service := &mcpActivityServiceStub{listed: []domainactivity.Activity{{ID: "activity-1", SpaceID: spaceID, Kind: "diaper"}}}

	got, err := ActivityList(mcpExternalContext(), resolver, service, ActivityListInput{
		Space: "Baby", Kind: " diaper ", From: "2026-09-22T00:00:00Z", To: "2026-09-23T00:00:00Z", Limit: 10,
	})
	if err != nil {
		t.Fatalf("activity_list: %v", err)
	}
	if len(got) != 1 || service.listCalls != 1 || service.listInput.Space != spaceID || service.listInput.Kind != " diaper " || service.listInput.Limit != 10 || service.listInput.From == nil || service.listInput.To == nil {
		t.Fatalf("output/service args = %+v/%+v", got, service)
	}
}

func TestActivityToolsRequireLinkedActor(t *testing.T) {
	resolver := &mcpExternalResolverStub{err: serviceidentity.ErrUnauthenticated}
	service := &mcpActivityServiceStub{}
	if _, err := ActivityCreate(mcpExternalContext(), resolver, service, ActivityCreateInput{Kind: "note", Note: "x", OccurredAt: "2026-09-22T08:30:00Z"}); err == nil {
		t.Fatal("activity_create accepted unlinked actor")
	} else {
		var mapped *MCPError
		if !errors.As(err, &mapped) || mapped.Code != "unauthenticated" {
			t.Fatalf("error = %T %v, want unauthenticated", err, err)
		}
	}
	if service.createCalls != 0 {
		t.Fatal("unlinked actor reached activity service")
	}
}

func TestActivityUpdateMapsPatchAndSelectedSpace(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000501"
	activityID := "00000000-0000-0000-0000-000000000601"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", spaceID, "Baby", domainspace.TypeShared, "role-member"),
		}},
		permissions: []domainpermission.Permission{{Resource: "activities", Action: "update"}},
	}
	service := &mcpActivityServiceStub{}
	note := "updated note"
	got, err := ActivityUpdate(mcpExternalContext(), resolver, service, ActivityUpdateInput{Space: "Baby", ActivityID: activityID, Note: &note, OccurredAt: "2026-09-22T09:00:00+07:00"})
	if err != nil {
		t.Fatalf("activity_update: %v", err)
	}
	if got == nil || got.ID != activityID || service.updateCalls != 1 || service.updateSpace != spaceID || service.updateID != activityID || service.updateInput.Note == nil || *service.updateInput.Note != note || service.updateInput.OccurredAt == nil {
		t.Fatalf("output/service args = %+v/%+v", got, service)
	}
}

func TestActivityUpdateRejectsEmptyPatchBeforeService(t *testing.T) {
	service := &mcpActivityServiceStub{}
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-1", "space-1", "Personal", domainspace.TypePersonal, "role-personal"),
		}},
		permissions: []domainpermission.Permission{{Resource: "activities", Action: "update"}},
	}
	_, err := ActivityUpdate(mcpExternalContext(), resolver, service, ActivityUpdateInput{ActivityID: "00000000-0000-0000-0000-000000000601"})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "invalid_input" {
		t.Fatalf("error = %T %v, want invalid_input", err, err)
	}
	if service.updateCalls != 0 {
		t.Fatal("empty update reached service")
	}
}

func TestActivityDeletePassesSelectedSpaceAndID(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000501"
	activityID := "00000000-0000-0000-0000-000000000601"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", spaceID, "Baby", domainspace.TypeShared, "role-member"),
		}},
		permissions: []domainpermission.Permission{{Resource: "activities", Action: "delete"}},
	}
	service := &mcpActivityServiceStub{}
	got, err := ActivityDelete(mcpExternalContext(), resolver, service, ActivityDeleteInput{Space: spaceID, ActivityID: activityID})
	if err != nil {
		t.Fatalf("activity_delete: %v", err)
	}
	if got == nil || got.ID != activityID || service.deleteCalls != 1 || service.deleteSpace != spaceID || service.deleteID != activityID {
		t.Fatalf("output/service args = %+v/%+v", got, service)
	}
}

var _ interfaceactivity.ServiceActivityInterface = (*mcpActivityServiceStub)(nil)
