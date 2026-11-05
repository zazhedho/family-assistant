package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	serviceauthorization "family-assistant/internal/services/authorization"
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
	reportActor domainidentity.ActorContext
	reportInput dto.FeedingLeftoverInput
	reportCalls int
	reported    *domainactivity.FeedingReportResult
	reportErr   error
}

func (s *mcpActivityServiceStub) ReportFeedingLeftover(_ context.Context, actor domainidentity.ActorContext, input dto.FeedingLeftoverInput) (*domainactivity.FeedingReportResult, error) {
	s.reportCalls++
	s.reportActor, s.reportInput = actor, input
	return s.reported, s.reportErr
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

func TestActivityCreateUsesMessageTimeOnlyWhenEventTimeOmitted(t *testing.T) {
	messageAt := time.Date(2026, 9, 29, 23, 59, 0, 0, time.FixedZone("WIB", 7*60*60))
	for _, scenario := range []struct {
		name, occurredAt string
		messageAt        time.Time
		want             time.Time
		invalid          bool
	}{
		{name: "now", messageAt: messageAt, want: messageAt},
		{name: "explicit", occurredAt: "2026-09-28T08:25:00+07:00", messageAt: messageAt, want: time.Date(2026, 9, 28, 1, 25, 0, 0, time.UTC)},
		{name: "missing trusted time", invalid: true},
		{name: "invalid explicit", occurredAt: "tomorrow", messageAt: messageAt, invalid: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			resolver := activityReminderResolver()
			service := &mcpActivityServiceStub{created: &domainactivity.Activity{ID: "activity-1", SpaceID: mcpSpaceID}}
			request, _ := ExternalRequestFromContext(mcpExternalContext())
			request.MessageAt = scenario.messageAt
			ctx := WithExternalRequest(context.Background(), request)
			_, err := ActivityCreate(ctx, resolver, service, ActivityCreateInput{Kind: "note", Note: "event", OccurredAt: scenario.occurredAt})
			if scenario.invalid {
				var mapped *MCPError
				if !errors.As(err, &mapped) || mapped.Code != "invalid_input" || service.createCalls != 0 {
					t.Fatalf("error = %v, calls = %d", err, service.createCalls)
				}
				return
			}
			if err != nil || service.createCalls != 1 || !service.createInput.OccurredAt.Equal(scenario.want) {
				t.Fatalf("occurred_at = %s, want %s; error = %v", service.createInput.OccurredAt, scenario.want, err)
			}
		})
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
		Space: "Baby", Kind: " diaper ", Search: "wet diaper", From: "2026-09-22T00:00:00Z", To: "2026-09-23T00:00:00Z", Limit: 10,
	})
	if err != nil {
		t.Fatalf("activity_list: %v", err)
	}
	if len(got) != 1 || service.listCalls != 1 || service.listInput.Space != spaceID || service.listInput.Kind != " diaper " || service.listInput.Limit != 10 || service.listInput.From == nil || service.listInput.To == nil {
		t.Fatalf("output/service args = %+v/%+v", got, service)
	}
	if service.listInput.Search != "wet diaper" {
		t.Fatalf("search = %q, want wet diaper", service.listInput.Search)
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

func feedingReportResolver(spaceID string) *mcpExternalResolverStub {
	return &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", spaceID, "Baby", domainspace.TypeShared, "role-member"),
		}},
		permissions: []domainpermission.Permission{{Resource: "activities", Action: "update"}},
	}
}

func feedingReportContext(messageAt time.Time) context.Context {
	return WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp", MessageAt: messageAt})
}

func TestFeedingLeftoverReportHandler(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000501"
	messageAt := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)
	ten := 10
	t.Run("success and corrected flag", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			previous domainactivity.Details
			changed  bool
			want     bool
		}{
			{"first report", domainactivity.Details{"volume_ml": 100}, true, false},
			{"overwrite", domainactivity.Details{"volume_ml": 100, "leftover_ml": 5}, true, true},
			{"overwrite outcome", domainactivity.Details{"outcome": "finished"}, true, true},
			{"identical repeat", domainactivity.Details{"leftover_ml": 10}, false, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				service := &mcpActivityServiceStub{reported: &domainactivity.FeedingReportResult{
					Activity: domainactivity.Activity{ID: "feeding-1", SpaceID: spaceID, Kind: "feeding"}, Previous: tt.previous, Changed: tt.changed,
				}}
				got, err := FeedingLeftoverReport(feedingReportContext(messageAt), feedingReportResolver(spaceID), service, FeedingLeftoverReportInput{
					Space: "Baby", FeedingID: "feeding-1", FeedingTime: "2026-11-05T08:00:00+07:00", LeftoverML: &ten,
				})
				if err != nil {
					t.Fatalf("report: %v", err)
				}
				if got.Activity.ID != "feeding-1" || got.Corrected != tt.want {
					t.Fatalf("output = %+v, want corrected %v", got, tt.want)
				}
				in := service.reportInput
				if in.Space != spaceID || in.FeedingID != "feeding-1" || !in.MessageAt.Equal(messageAt) || in.LeftoverML == nil || *in.LeftoverML != 10 ||
					in.FeedingTime == nil || !in.FeedingTime.Equal(time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)) {
					t.Fatalf("service input = %+v", in)
				}
			})
		}
	})
	t.Run("target errors", func(t *testing.T) {
		wib := time.FixedZone("WIB", 7*3600)
		vol := 120
		for _, tt := range []struct{ reason, code string }{{"ambiguous", "ambiguous_feeding"}, {"none", "no_feeding_match"}} {
			service := &mcpActivityServiceStub{reportErr: &domainactivity.FeedingTargetError{Reason: tt.reason, Candidates: []domainactivity.FeedingCandidate{
				{ID: "feed-aaa", OccurredAt: time.Date(2026, 11, 5, 8, 5, 0, 0, wib), VolumeML: &vol, Open: true},
				{ID: "feed-bbb", OccurredAt: time.Date(2026, 11, 5, 9, 30, 0, 0, wib)},
			}}}
			_, err := FeedingLeftoverReport(feedingReportContext(messageAt), feedingReportResolver(spaceID), service, FeedingLeftoverReportInput{Space: "Baby", LeftoverML: &ten})
			var mapped *MCPError
			if !errors.As(err, &mapped) || mapped.Code != tt.code {
				t.Fatalf("%s: error = %v", tt.reason, err)
			}
			for _, want := range []string{"feed-aaa, 05/11 08:05 WIB, 120 ml, open", "feed-bbb, 05/11 09:30 WIB, volume unknown, closed", "feeding_id", "ask the user"} {
				if !strings.Contains(strings.ToLower(mapped.Message), strings.ToLower(want)) {
					t.Fatalf("%s: message %q lacks %q", tt.reason, mapped.Message, want)
				}
			}
		}
	})
	t.Run("missing message time", func(t *testing.T) {
		service := &mcpActivityServiceStub{}
		_, err := FeedingLeftoverReport(mcpExternalContext(), feedingReportResolver(spaceID), service, FeedingLeftoverReportInput{Space: "Baby", LeftoverML: &ten})
		var mapped *MCPError
		if !errors.As(err, &mapped) || mapped.Code != "invalid_input" || service.reportCalls != 0 {
			t.Fatalf("error = %v calls = %d", err, service.reportCalls)
		}
	})
	t.Run("unparseable feeding_time", func(t *testing.T) {
		service := &mcpActivityServiceStub{}
		_, err := FeedingLeftoverReport(feedingReportContext(messageAt), feedingReportResolver(spaceID), service, FeedingLeftoverReportInput{Space: "Baby", FeedingTime: "yesterday", LeftoverML: &ten})
		var mapped *MCPError
		if !errors.As(err, &mapped) || mapped.Code != "invalid_input" || service.reportCalls != 0 {
			t.Fatalf("error = %v calls = %d", err, service.reportCalls)
		}
	})
	t.Run("bad outcome reaches service validation", func(t *testing.T) {
		service := &mcpActivityServiceStub{reportErr: &serviceauthorization.ValidationError{Field: "outcome", Reason: "must be finished or expired"}}
		_, err := FeedingLeftoverReport(feedingReportContext(messageAt), feedingReportResolver(spaceID), service, FeedingLeftoverReportInput{Space: "Baby", Outcome: "spilled"})
		var mapped *MCPError
		if !errors.As(err, &mapped) || mapped.Code != "invalid_input" || service.reportCalls != 1 || service.reportInput.Outcome != "spilled" {
			t.Fatalf("error = %v calls = %d input = %+v", err, service.reportCalls, service.reportInput)
		}
	})
	t.Run("other errors mapped", func(t *testing.T) {
		service := &mcpActivityServiceStub{reportErr: errors.New("boom")}
		_, err := FeedingLeftoverReport(feedingReportContext(messageAt), feedingReportResolver(spaceID), service, FeedingLeftoverReportInput{Space: "Baby", LeftoverML: &ten})
		var mapped *MCPError
		if !errors.As(err, &mapped) || mapped.Code != "internal" {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestActivityCreatePassesDetailsAndSubject(t *testing.T) {
	resolver := activityReminderResolver()
	service := &mcpActivityServiceStub{created: &domainactivity.Activity{ID: "activity-1", SpaceID: mcpSpaceID}}
	_, err := ActivityCreate(feedingReportContext(time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC)), resolver, service, ActivityCreateInput{
		Kind: "medication", Subject: "Aisha", Details: map[string]any{"name": "paracetamol"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if service.createInput.Subject != "Aisha" || service.createInput.Details["name"] != "paracetamol" {
		t.Fatalf("input = %+v", service.createInput)
	}
}

func TestActivityUpdatePassesDetailsAndSubject(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000501"
	service := &mcpActivityServiceStub{}
	subject := "Aisha"
	_, err := ActivityUpdate(mcpExternalContext(), feedingReportResolver(spaceID), service, ActivityUpdateInput{
		Space: "Baby", ActivityID: "00000000-0000-0000-0000-000000000601", Subject: &subject, Details: map[string]any{"celsius": 38.5},
	})
	if err != nil || service.updateInput.Subject == nil || *service.updateInput.Subject != "Aisha" || service.updateInput.Details["celsius"] != 38.5 {
		t.Fatalf("err = %v input = %+v", err, service.updateInput)
	}
}

func TestFeedingLeftoverReportToolErrorReachesModel(t *testing.T) {
	vol := 120
	for _, tt := range []struct{ reason, code string }{{"ambiguous", "ambiguous_feeding"}, {"none", "no_feeding_match"}} {
		t.Run(tt.reason, func(t *testing.T) {
			service := &mcpActivityServiceStub{reportErr: &domainactivity.FeedingTargetError{Reason: tt.reason, Candidates: []domainactivity.FeedingCandidate{
				{ID: "feed-aaa", OccurredAt: time.Date(2026, 11, 5, 1, 5, 0, 0, time.UTC), VolumeML: &vol, Open: true},
				{ID: "feed-bbb", OccurredAt: time.Date(2026, 11, 5, 2, 30, 0, 0, time.UTC)},
			}}}
			resolver := activityReminderResolver()
			resolver.permissions = append(resolver.permissions, domainpermission.Permission{Resource: "activities", Action: "update"})
			server := activityReminderHTTPServer(t, resolver, service, &reminderToolServiceStub{}, "identity-secret")
			identity := newSignedIdentityEnvelope("identity-secret", time.Now())
			identity.Version, identity.MessageAt = "v2", time.Now().Unix()
			identity.Signature = signIdentityEnvelope("identity-secret", identity)
			response := callMCPServer(t, server.URL, "profile", "secret", "tools/call", map[string]any{
				"name": "feeding_leftover_report", "arguments": map[string]any{"leftover_ml": 20, identityArgumentName: identity},
			})
			if response.Result == nil || !response.Result.IsError || len(response.Result.Content) == 0 {
				t.Fatalf("response = %+v", response)
			}
			text := response.Result.Content[0].Text
			for _, want := range []string{"feed-aaa, 05/11 08:05 WIB, 120 ml, open", "feed-bbb, 05/11 09:30 WIB, volume unknown, closed", "feeding_id", "Ask the user"} {
				if !strings.Contains(text, want) {
					t.Fatalf("tool error text %q lacks %q", text, want)
				}
			}
			if !strings.HasPrefix(text, tt.code+":") {
				t.Fatalf("tool error text %q does not start with code %q", text, tt.code)
			}
		})
	}
}
