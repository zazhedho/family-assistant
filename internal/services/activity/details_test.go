package serviceactivity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	"family-assistant/internal/services/authorization"
	"gorm.io/gorm"
)

const (
	testSpace   = "space-1"
	testFeeding = "00000000-0000-0000-0000-0000000000f1"
)

func ownerSetup(found *domainactivity.Activity) (*activityRepositoryStub, *activitySpaceRepositoryStub, *activityAuditStub) {
	repo := &activityRepositoryStub{found: found}
	spaces := &activitySpaceRepositoryStub{members: []domainspace.ResolvedMembership{
		{ID: "member-1", SpaceID: testSpace, UserID: "user-1", RoleName: "space_owner", Status: domainspace.StatusActive},
	}}
	return repo, spaces, &activityAuditStub{}
}

func ownerActor(permissions ...string) domainidentity.ActorContext {
	actor := activityActor(testSpace, "member-1", permissions...)
	actor.RoleName = "space_owner"
	return actor
}

func requireValidation(t *testing.T, err error, field string) {
	t.Helper()
	var validationErr *authorization.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T %v, want *ValidationError", err, err)
	}
	if field != "" && validationErr.Field != field {
		t.Fatalf("validation field = %q, want %q (%v)", validationErr.Field, field, err)
	}
}

func intPtr(v int) *int { return &v }

func TestCreateNormalizesKindAndValidatesDetails(t *testing.T) {
	repo, spaces, audit := ownerSetup(nil)
	service := activityService(repo, spaces, audit)
	at := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)

	created, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{Kind: " Bathing ", Note: "warm bath", OccurredAt: at})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.created == nil || repo.created.Kind != "bath" || created.Kind != "bath" {
		t.Fatalf("kind = %+v, want bath", repo.created)
	}

	repo.created = nil
	_, err = service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{
		Kind: "note", Note: "big", OccurredAt: at, Details: map[string]any{"blob": strings.Repeat("x", domainactivity.MaxDetailsJSONBytes)},
	})
	requireValidation(t, err, "details")
	if repo.created != nil {
		t.Fatal("oversize details were persisted")
	}

	_, err = service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{
		Kind: "temperature", Note: "hot", OccurredAt: at, Details: map[string]any{"celsius": 50},
	})
	requireValidation(t, err, "details.celsius")
	if repo.created != nil {
		t.Fatal("invalid details were persisted")
	}
	if last := audit.events[len(audit.events)-1]; last.Status != domainaudit.StatusFailed || last.ErrorMessage != "validation" {
		t.Fatalf("failure audit = %+v", last)
	}
}

func TestCreateFeedingGeneratesNote(t *testing.T) {
	repo, spaces, audit := ownerSetup(nil)
	service := activityService(repo, spaces, audit)
	at := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)

	created, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{
		Kind: "bottle feeding", Subject: " Aisha ", Note: "agent wrote this", OccurredAt: at, Details: map[string]any{"volume_ml": float64(60)},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := domainactivity.FeedingNote("Aisha", domainactivity.Details{"volume_ml": 60})
	if repo.created.Kind != "feeding" || repo.created.Subject != "Aisha" || repo.created.Note != want || created.Note != want {
		t.Fatalf("created = %+v, want note %q", repo.created, want)
	}
	if v, ok := repo.created.Details["volume_ml"]; !ok || v != float64(60) {
		t.Fatalf("details = %+v", repo.created.Details)
	}
	if _, ok := repo.created.Details["leftover_reported_at"]; ok {
		t.Fatalf("leftover_reported_at set without a report: %+v", repo.created.Details)
	}

	// The generated note replaces the "note is required" rule.
	repo.created = nil
	if _, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{
		Kind: "feeding", OccurredAt: at, Details: map[string]any{"volume_ml": 90},
	}); err != nil {
		t.Fatalf("create without note: %v", err)
	}
	if repo.created == nil || repo.created.Note != domainactivity.FeedingNote("", domainactivity.Details{"volume_ml": 90}) {
		t.Fatalf("created = %+v", repo.created)
	}
}

func TestCreateFeedingWithoutDetailsKeepsSuppliedNote(t *testing.T) {
	repo, spaces, audit := ownerSetup(nil)
	service := activityService(repo, spaces, audit)
	at := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)

	if _, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{Kind: "feeding", Note: " 60ml sufor ", OccurredAt: at}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.created.Note != "60ml sufor" || repo.created.Kind != "feeding" || len(repo.created.Details) != 0 {
		t.Fatalf("created = %+v", repo.created)
	}

	repo.created = nil
	_, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{Kind: "feeding", OccurredAt: at})
	requireValidation(t, err, "note")
	if repo.created != nil {
		t.Fatal("feeding without details or note was persisted")
	}
}

func TestCreateFeedingWithLeftoverInOneCall(t *testing.T) {
	at := time.Date(2026, 11, 5, 8, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	stamp := at.UTC().Format(time.RFC3339)
	for _, tc := range []struct {
		name    string
		details map[string]any
		want    domainactivity.Details
	}{
		{name: "leftover", details: map[string]any{"volume_ml": 60, "leftover_ml": 35}, want: domainactivity.Details{"volume_ml": 60, "leftover_ml": 35}},
		{name: "finished", details: map[string]any{"volume_ml": 60, "outcome": "finished"}, want: domainactivity.Details{"volume_ml": 60, "outcome": "finished", "expired_ml": 0}},
		{name: "expired", details: map[string]any{"volume_ml": float64(60), "outcome": "expired", "expired_ml": float64(30)}, want: domainactivity.Details{"volume_ml": 60, "outcome": "expired", "expired_ml": 30}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, spaces, audit := ownerSetup(nil)
			service := activityService(repo, spaces, audit)
			if _, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{
				Kind: "feeding", Subject: "Aisha", Note: "ignored", OccurredAt: at, Details: tc.details,
			}); err != nil {
				t.Fatalf("create: %v", err)
			}
			got := repo.created.Details
			if got["leftover_reported_at"] != stamp {
				t.Fatalf("leftover_reported_at = %v, want %s", got["leftover_reported_at"], stamp)
			}
			for key, want := range tc.want {
				if !sameNumberOrString(got[key], want) {
					t.Fatalf("details[%s] = %v, want %v (%+v)", key, got[key], want, got)
				}
			}
			if repo.created.Note != domainactivity.FeedingNote("Aisha", got) {
				t.Fatalf("note = %q, want generated", repo.created.Note)
			}
		})
	}

	for _, tc := range []struct {
		name    string
		details map[string]any
		field   string
	}{
		{name: "leftover above volume", details: map[string]any{"volume_ml": 60, "leftover_ml": 70}, field: "details.leftover_ml"},
		{name: "finished with expired", details: map[string]any{"volume_ml": 60, "outcome": "finished", "expired_ml": 10}, field: "details.expired_ml"},
		{name: "expired without amount", details: map[string]any{"volume_ml": 60, "outcome": "expired"}, field: "details.expired_ml"},
		{name: "leftover without volume", details: map[string]any{"leftover_ml": 20}, field: "details.volume_ml"},
		{name: "client reported_at", details: map[string]any{"volume_ml": 60, "leftover_ml": 20, "leftover_reported_at": stamp}, field: "details.leftover_reported_at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, spaces, audit := ownerSetup(nil)
			service := activityService(repo, spaces, audit)
			_, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{
				Kind: "feeding", OccurredAt: at, Details: tc.details,
			})
			requireValidation(t, err, tc.field)
			if repo.created != nil {
				t.Fatal("invalid feeding was persisted")
			}
		})
	}
}

func sameNumberOrString(got, want any) bool {
	if s, ok := want.(string); ok {
		return got == s
	}
	w, _ := want.(int)
	switch g := got.(type) {
	case int:
		return g == w
	case float64:
		return g == float64(w)
	}
	return false
}

func TestCreateRejectsLongSubject(t *testing.T) {
	repo, spaces, audit := ownerSetup(nil)
	service := activityService(repo, spaces, audit)
	at := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)

	_, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{Kind: "note", Note: "n", Subject: strings.Repeat("é", 129), OccurredAt: at})
	requireValidation(t, err, "subject")
	if _, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{Kind: "note", Note: "n", Subject: strings.Repeat("é", 128), OccurredAt: at}); err != nil {
		t.Fatalf("128-character subject rejected: %v", err)
	}
}

func TestUpdateRejectsReportedKeysOnFeeding(t *testing.T) {
	for _, key := range domainactivity.FeedingReportedKeys() {
		t.Run(key, func(t *testing.T) {
			repo, spaces, audit := ownerSetup(&domainactivity.Activity{ID: "activity-1", SpaceID: testSpace, CreatedByMemberID: "member-1", Kind: "feeding", Details: domainactivity.Details{"volume_ml": 60}})
			service := activityService(repo, spaces, audit)
			_, err := service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", dto.ActivityUpdateInput{Details: map[string]any{key: 10}})
			requireValidation(t, err, "details")
			if repo.updated {
				t.Fatal("reported key reached the repository")
			}
		})
	}
	// Changing a row into a feeding is covered by the same rule.
	repo, spaces, audit := ownerSetup(&domainactivity.Activity{ID: "activity-1", SpaceID: testSpace, CreatedByMemberID: "member-1", Kind: "note", Note: "x"})
	service := activityService(repo, spaces, audit)
	kind := "bottle_feeding"
	_, err := service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", dto.ActivityUpdateInput{Kind: &kind, Details: map[string]any{"volume_ml": 60, "outcome": "finished"}})
	requireValidation(t, err, "details")
	if repo.updated {
		t.Fatal("reported key reached the repository")
	}
}

func TestUpdateMergesDetails(t *testing.T) {
	existing := &domainactivity.Activity{
		ID: "activity-1", SpaceID: testSpace, CreatedByMemberID: "member-1", Kind: "feeding", Subject: "Aisha",
		Details: domainactivity.Details{"volume_ml": float64(60), "leftover_ml": float64(20), "leftover_reported_at": "2026-11-05T08:30:00Z"},
	}
	existing.Note = domainactivity.FeedingNote(existing.Subject, existing.Details)
	repo, spaces, audit := ownerSetup(existing)
	service := activityService(repo, spaces, audit)

	got, err := service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": 90}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(repo.update.DetailsPatch) != 1 || repo.update.DetailsPatch["volume_ml"] != 90 {
		t.Fatalf("patch = %+v, want only volume_ml", repo.update.DetailsPatch)
	}
	merged := domainactivity.Details{"volume_ml": 90, "leftover_ml": float64(20), "leftover_reported_at": "2026-11-05T08:30:00Z"}
	wantNote := domainactivity.FeedingNote("Aisha", merged)
	if repo.update.Note == nil || *repo.update.Note != wantNote || got.Note != wantNote {
		t.Fatalf("note = %v / %q, want %q", repo.update.Note, got.Note, wantNote)
	}
	if got.Details["volume_ml"] != 90 || got.Details["leftover_ml"] != float64(20) {
		t.Fatalf("merged details = %+v", got.Details)
	}

	// A volume below the stored leftover is rejected against the merged state.
	repo.updated = false
	_, err = service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": 10}})
	requireValidation(t, err, "details.leftover_ml")
	if repo.updated {
		t.Fatal("invalid merge reached the repository")
	}

	// Subject alone regenerates the note; a nil value removes a key.
	repo.updated = false
	subject := "Bima"
	got, err = service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", dto.ActivityUpdateInput{Subject: &subject})
	if err != nil {
		t.Fatalf("update subject: %v", err)
	}
	if repo.update.Subject == nil || *repo.update.Subject != "Bima" || got.Subject != "Bima" || got.Note != domainactivity.FeedingNote("Bima", existing.Details) {
		t.Fatalf("subject update = %+v / %+v", repo.update, got)
	}

	medication := &domainactivity.Activity{ID: "activity-2", SpaceID: testSpace, CreatedByMemberID: "member-1", Kind: "medication", Note: "paracetamol", Details: domainactivity.Details{"name": "paracetamol", "dose": 5}}
	repo, spaces, audit = ownerSetup(medication)
	service = activityService(repo, spaces, audit)
	got, err = service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-2", dto.ActivityUpdateInput{Details: map[string]any{"dose": nil}})
	if err != nil {
		t.Fatalf("remove key: %v", err)
	}
	if _, ok := got.Details["dose"]; ok || got.Note != "paracetamol" || repo.update.Note != nil {
		t.Fatalf("removal result = %+v, fields = %+v", got, repo.update)
	}
	_, err = service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-2", dto.ActivityUpdateInput{Details: map[string]any{"name": nil}})
	requireValidation(t, err, "details.name")

	long := strings.Repeat("a", 129)
	_, err = service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-2", dto.ActivityUpdateInput{Subject: &long})
	requireValidation(t, err, "subject")
}

func TestListNormalizesKindFilter(t *testing.T) {
	repo, spaces, audit := ownerSetup(nil)
	service := activityService(repo, spaces, audit)
	if _, err := service.List(context.Background(), ownerActor("activities:list"), dto.ActivityListInput{Kind: " Bottle Feeding "}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if repo.filter.Kind != "feeding" {
		t.Fatalf("kind filter = %q, want feeding", repo.filter.Kind)
	}
}

func feedingInput() dto.FeedingLeftoverInput {
	return dto.FeedingLeftoverInput{Space: testSpace, MessageAt: time.Date(2026, 11, 5, 9, 0, 0, 0, time.FixedZone("WIB", 7*3600)), LeftoverML: intPtr(30)}
}

func TestReportFeedingLeftoverAuthorizesAndAudits(t *testing.T) {
	feeding := domainactivity.Activity{ID: testFeeding, SpaceID: testSpace, CreatedByMemberID: "member-1", Kind: "feeding", Details: domainactivity.Details{"volume_ml": 60}}

	t.Run("success", func(t *testing.T) {
		repo, spaces, audit := ownerSetup(nil)
		updated := feeding
		updated.Details = domainactivity.Details{"volume_ml": 60, "leftover_ml": 30}
		repo.reportTarget = &feeding
		repo.reportResult = &domainactivity.FeedingReportResult{Activity: updated, Previous: domainactivity.Details{"volume_ml": 60}, Changed: true}
		service := activityService(repo, spaces, audit)
		input := feedingInput()
		input.Outcome = " finished "
		before := time.Now().UTC()

		got, err := service.ReportFeedingLeftover(context.Background(), ownerActor("activities:update"), input)
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		if got == nil || got.Activity.ID != testFeeding || !got.Changed {
			t.Fatalf("result = %+v", got)
		}
		req := repo.reportReq
		if req.SpaceID != testSpace || !req.Selector.MessageAt.Equal(input.MessageAt) || !req.ReportedAt.Equal(input.MessageAt) || req.ReportedAt.Location() != time.UTC {
			t.Fatalf("request = %+v", req)
		}
		if req.Now.Before(before) || req.Now.Location() != time.UTC || req.CanMutate == nil || req.Report.LeftoverML == nil || *req.Report.LeftoverML != 30 || req.Report.Outcome != "finished" {
			t.Fatalf("request = %+v", req)
		}
		last := audit.events[len(audit.events)-1]
		if last.Action != domainaudit.ActionUpdate || last.Status != domainaudit.StatusSuccess || last.ResourceID != testFeeding {
			t.Fatalf("audit = %+v", last)
		}
		if prev, ok := last.Metadata["previous_details"].(domainactivity.Details); !ok || prev["volume_ml"] != 60 || last.Metadata["changed"] != true {
			t.Fatalf("audit metadata = %+v", last.Metadata)
		}
	})

	t.Run("viewer without permission", func(t *testing.T) {
		repo, spaces, audit := ownerSetup(nil)
		spaces.members[0].RoleName = "space_viewer"
		service := activityService(repo, spaces, audit)
		actor := activityActor(testSpace, "member-1", "activities:list")
		_, err := service.ReportFeedingLeftover(context.Background(), actor, feedingInput())
		if !errors.Is(err, authorization.ErrForbidden) || repo.reportCalls != 0 {
			t.Fatalf("error = %v calls = %d, want forbidden before repository", err, repo.reportCalls)
		}
		if last := audit.events[len(audit.events)-1]; last.Action != domainaudit.ActionUpdate || last.ErrorMessage != "forbidden" {
			t.Fatalf("audit = %+v", last)
		}
	})

	t.Run("viewer role cannot mutate", func(t *testing.T) {
		repo, spaces, audit := ownerSetup(nil)
		spaces.members[0].RoleName = "space_viewer"
		repo.reportTarget = &feeding
		repo.reportResult = &domainactivity.FeedingReportResult{Activity: feeding}
		service := activityService(repo, spaces, audit)
		_, err := service.ReportFeedingLeftover(context.Background(), activityActor(testSpace, "member-1", "activities:update"), feedingInput())
		if !errors.Is(err, authorization.ErrForbidden) {
			t.Fatalf("error = %v, want forbidden", err)
		}
	})

	t.Run("member on another member's feeding", func(t *testing.T) {
		repo, spaces, audit := ownerSetup(nil)
		spaces.members[0].RoleName = "space_member"
		other := feeding
		other.CreatedByMemberID = "member-2"
		repo.reportTarget = &other
		repo.reportResult = &domainactivity.FeedingReportResult{Activity: other}
		service := activityService(repo, spaces, audit)
		_, err := service.ReportFeedingLeftover(context.Background(), activityActor(testSpace, "member-1", "activities:update"), feedingInput())
		if !errors.Is(err, authorization.ErrForbidden) {
			t.Fatalf("error = %v, want forbidden", err)
		}
	})

	t.Run("non-member", func(t *testing.T) {
		repo, spaces, audit := ownerSetup(nil)
		spaces.members[0].SpaceID = "space-2"
		service := activityService(repo, spaces, audit)
		_, err := service.ReportFeedingLeftover(context.Background(), ownerActor("activities:update"), feedingInput())
		if !errors.Is(err, authorization.ErrNotFound) || repo.reportCalls != 0 {
			t.Fatalf("error = %v calls = %d, want not found", err, repo.reportCalls)
		}
	})
}

func TestReportFeedingLeftoverMapsTargetErrors(t *testing.T) {
	ambiguous := &domainactivity.FeedingTargetError{Reason: "ambiguous", Candidates: []domainactivity.FeedingCandidate{{ID: "a"}, {ID: "b"}}}
	for _, tc := range []struct {
		name  string
		err   error
		check func(*testing.T, error)
		cat   string
	}{
		{name: "ambiguous passes through", err: ambiguous, cat: "validation", check: func(t *testing.T, err error) {
			var target *domainactivity.FeedingTargetError
			if !errors.As(err, &target) || target != ambiguous || len(target.Candidates) != 2 {
				t.Fatalf("error = %T %v, want the same *FeedingTargetError", err, err)
			}
		}},
		{name: "details error", err: &domainactivity.DetailsError{Field: "expired_ml", Reason: "is required when outcome is expired"}, cat: "validation", check: func(t *testing.T, err error) {
			requireValidation(t, err, "expired_ml")
		}},
		{name: "forbidden", err: domainactivity.ErrFeedingForbidden, cat: "forbidden", check: func(t *testing.T, err error) {
			if !errors.Is(err, authorization.ErrForbidden) {
				t.Fatalf("error = %v, want forbidden", err)
			}
		}},
		{name: "not found", err: gorm.ErrRecordNotFound, cat: "not_found", check: func(t *testing.T, err error) {
			if !errors.Is(err, authorization.ErrNotFound) {
				t.Fatalf("error = %v, want not found", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, spaces, audit := ownerSetup(nil)
			repo.reportErr = tc.err
			service := activityService(repo, spaces, audit)
			_, err := service.ReportFeedingLeftover(context.Background(), ownerActor("activities:update"), feedingInput())
			tc.check(t, err)
			last := audit.events[len(audit.events)-1]
			if last.Action != domainaudit.ActionUpdate || last.Status != domainaudit.StatusFailed || last.ErrorMessage != tc.cat {
				t.Fatalf("audit = %+v, want category %s", last, tc.cat)
			}
		})
	}
}

func TestReportFeedingLeftoverValidatesInput(t *testing.T) {
	msg := feedingInput().MessageAt
	at := func(d time.Duration) *time.Time { v := msg.Add(d); return &v }
	for _, tc := range []struct {
		name   string
		mutate func(*dto.FeedingLeftoverInput)
		field  string
	}{
		{name: "missing message time", mutate: func(in *dto.FeedingLeftoverInput) { in.MessageAt = time.Time{} }, field: "message_at"},
		{name: "bad feeding id", mutate: func(in *dto.FeedingLeftoverInput) { in.FeedingID = "feeding-1" }, field: "feeding_id"},
		{name: "braced feeding id", mutate: func(in *dto.FeedingLeftoverInput) { in.FeedingID = "{" + testFeeding + "}" }, field: "feeding_id"},
		{name: "feeding time too old", mutate: func(in *dto.FeedingLeftoverInput) { in.FeedingTime = at(-48*time.Hour - time.Minute) }, field: "feeding_time"},
		{name: "feeding time in the future", mutate: func(in *dto.FeedingLeftoverInput) {
			in.FeedingTime = at(domainactivity.FeedingTimeTolerance + time.Minute)
		}, field: "feeding_time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, spaces, audit := ownerSetup(nil)
			service := activityService(repo, spaces, audit)
			input := feedingInput()
			tc.mutate(&input)
			_, err := service.ReportFeedingLeftover(context.Background(), ownerActor("activities:update"), input)
			requireValidation(t, err, tc.field)
			if repo.reportCalls != 0 {
				t.Fatal("invalid input reached the repository")
			}
		})
	}

	for _, tc := range []struct {
		name  string
		input func() dto.FeedingLeftoverInput
	}{
		{name: "oldest feeding time", input: func() dto.FeedingLeftoverInput { in := feedingInput(); in.FeedingTime = at(-48 * time.Hour); return in }},
		{name: "latest feeding time", input: func() dto.FeedingLeftoverInput {
			in := feedingInput()
			in.FeedingTime = at(domainactivity.FeedingTimeTolerance)
			return in
		}},
		{name: "feeding id", input: func() dto.FeedingLeftoverInput {
			in := feedingInput()
			in.FeedingID = " " + testFeeding + " "
			return in
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, spaces, audit := ownerSetup(nil)
			repo.reportResult = &domainactivity.FeedingReportResult{Activity: domainactivity.Activity{ID: testFeeding}}
			service := activityService(repo, spaces, audit)
			input := tc.input()
			if _, err := service.ReportFeedingLeftover(context.Background(), ownerActor("activities:update"), input); err != nil {
				t.Fatalf("report: %v", err)
			}
			sel := repo.reportReq.Selector
			if input.FeedingTime != nil && (sel.FeedingTime == nil || !sel.FeedingTime.Equal(*input.FeedingTime)) {
				t.Fatalf("selector = %+v", sel)
			}
			if input.FeedingID != "" && sel.FeedingID != testFeeding {
				t.Fatalf("selector feeding id = %q", sel.FeedingID)
			}
		})
	}
}

func TestUpdateKindToFeedingRejectsStoredReportedKeys(t *testing.T) {
	forged := func() *domainactivity.Activity {
		return &domainactivity.Activity{ID: "activity-1", SpaceID: testSpace, CreatedByMemberID: "member-1", Kind: "note", Note: "x", Details: domainactivity.Details{
			"volume_ml": float64(60), "leftover_ml": float64(10), "outcome": "finished", "expired_ml": float64(0), "leftover_reported_at": "2020-01-01T00:00:00Z",
		}}
	}
	kind := "feeding"

	repo, spaces, audit := ownerSetup(forged())
	service := activityService(repo, spaces, audit)
	_, err := service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", dto.ActivityUpdateInput{Kind: &kind})
	requireValidation(t, err, "details")
	if repo.updated {
		t.Fatal("forged feeding report reached the repository")
	}

	repo, spaces, audit = ownerSetup(forged())
	service = activityService(repo, spaces, audit)
	removal := map[string]any{}
	for _, key := range domainactivity.FeedingReportedKeys() {
		removal[key] = nil
	}
	got, err := service.Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", dto.ActivityUpdateInput{Kind: &kind, Details: removal})
	if err != nil {
		t.Fatalf("update with reported keys removed: %v", err)
	}
	// The stored note was written by the caller, so it is kept.
	if !repo.updated || got.Kind != "feeding" || len(got.Details) != 1 || got.Note != "x" || repo.update.Note != nil {
		t.Fatalf("result = %+v, fields = %+v", got, repo.update)
	}
}

func TestReportFeedingLeftoverAuditsOnlyValidFeedingID(t *testing.T) {
	repo, spaces, audit := ownerSetup(nil)
	service := activityService(repo, spaces, audit)
	input := feedingInput()
	input.FeedingID = strings.Repeat("a", 500)
	_, err := service.ReportFeedingLeftover(context.Background(), ownerActor("activities:update"), input)
	requireValidation(t, err, "feeding_id")
	last := audit.events[len(audit.events)-1]
	if last.ResourceID != "" || last.ErrorMessage != "validation" {
		t.Fatalf("audit = %+v, want empty resource id", last)
	}
}

func TestUpdateFeedingRegeneratesOnlyGeneratedNotes(t *testing.T) {
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	legacy := func() *domainactivity.Activity {
		return &domainactivity.Activity{ID: "activity-1", SpaceID: testSpace, CreatedByMemberID: "member-1", Kind: "feeding",
			Note: "Feeding Baby Zeia — volume 60 ml, sisa 20", Details: domainactivity.Details{}}
	}
	update := func(t *testing.T, found *domainactivity.Activity, input dto.ActivityUpdateInput) (*domainactivity.Activity, *activityRepositoryStub) {
		t.Helper()
		repo, spaces, audit := ownerSetup(found)
		got, err := activityService(repo, spaces, audit).Update(context.Background(), ownerActor("activities:update"), testSpace, "activity-1", input)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		return got, repo
	}

	t.Run("legacy prose note survives a volume patch", func(t *testing.T) {
		got, repo := update(t, legacy(), dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": 60}})
		if repo.update.Note != nil || got.Note != legacy().Note {
			t.Fatalf("note = %v / %q, want prose kept", repo.update.Note, got.Note)
		}
	})
	t.Run("caller note survives an occurred_at-only update", func(t *testing.T) {
		row := legacy()
		row.Details = domainactivity.Details{"volume_ml": float64(60)}
		got, repo := update(t, row, dto.ActivityUpdateInput{OccurredAt: &at})
		if repo.update.Note != nil || got.Note != row.Note {
			t.Fatalf("note = %v / %q, want prose kept", repo.update.Note, got.Note)
		}
	})
	t.Run("generated note follows a volume change", func(t *testing.T) {
		row := legacy()
		row.Subject, row.Details = "Zeia", domainactivity.Details{"volume_ml": float64(60)}
		row.Note = domainactivity.FeedingNote(row.Subject, row.Details)
		got, repo := update(t, row, dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": 90}})
		want := domainactivity.FeedingNote("Zeia", domainactivity.Details{"volume_ml": 90})
		if repo.update.Note == nil || *repo.update.Note != want || got.Note != want {
			t.Fatalf("note = %v / %q, want %q", repo.update.Note, got.Note, want)
		}
	})
	t.Run("generated note drops a removed volume", func(t *testing.T) {
		row := legacy()
		row.Subject, row.Details = "Zeia", domainactivity.Details{"volume_ml": float64(60)}
		row.Note = domainactivity.FeedingNote(row.Subject, row.Details)
		got, repo := update(t, row, dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": nil}})
		want := domainactivity.FeedingNote("Zeia", domainactivity.Details{})
		if repo.update.Note == nil || *repo.update.Note != want || got.Note != want || strings.Contains(got.Note, "volume") {
			t.Fatalf("note = %v / %q, want %q", repo.update.Note, got.Note, want)
		}
	})
	t.Run("caller note on update is kept", func(t *testing.T) {
		row := legacy()
		row.Subject, row.Details = "Zeia", domainactivity.Details{"volume_ml": float64(60)}
		row.Note = domainactivity.FeedingNote(row.Subject, row.Details)
		note := "minum di mobil"
		got, repo := update(t, row, dto.ActivityUpdateInput{Note: &note, Details: map[string]any{"volume_ml": 90}})
		if repo.update.Note == nil || *repo.update.Note != note || got.Note != note {
			t.Fatalf("note = %v / %q, want caller note", repo.update.Note, got.Note)
		}
	})
}

func TestCreateFeedingStampsReportWithMessageTime(t *testing.T) {
	messageAt := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	occurredAt := messageAt.Add(-2 * time.Hour)
	repo, spaces, audit := ownerSetup(nil)
	service := activityService(repo, spaces, audit)
	if _, err := service.Create(context.Background(), ownerActor("activities:create"), dto.ActivityCreateInput{
		Kind: "feeding", OccurredAt: occurredAt, MessageAt: messageAt, Details: map[string]any{"volume_ml": 60, "leftover_ml": 20},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := repo.created.Details["leftover_reported_at"]; got != messageAt.Format(time.RFC3339) {
		t.Fatalf("leftover_reported_at = %v, want message time %s", got, messageAt.Format(time.RFC3339))
	}
	if !repo.created.OccurredAt.Equal(occurredAt) {
		t.Fatalf("occurred_at = %v, want %v", repo.created.OccurredAt, occurredAt)
	}
}
