package integration

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	activityRepo "family-assistant/internal/repositories/activity"
	spaceRepo "family-assistant/internal/repositories/space"
	activityService "family-assistant/internal/services/activity"
	"family-assistant/internal/services/authorization"
	"family-assistant/utils"

	"gorm.io/gorm"
)

type scenarioActor struct {
	actor    domainidentity.ActorContext
	memberID string
}

type scenarioSpace struct {
	id      string
	owner   scenarioActor
	member  scenarioActor
	viewer  scenarioActor
	outside scenarioActor
}

func TestFeedingLeftoverScenarios(t *testing.T) {
	rawDSN, dsnSet := os.LookupEnv("FAMILY_ASSISTANT_TEST_DATABASE_URL")
	dsn := strings.TrimSpace(rawDSN)
	if !dsnSet {
		t.Skip("FAMILY_ASSISTANT_TEST_DATABASE_URL is not set")
	}
	if dsn == "" {
		t.Fatal("FAMILY_ASSISTANT_TEST_DATABASE_URL must not be empty when set")
	}
	assertSafeTestDSN(t, dsn)

	db := resetIntegrationDatabase(t, dsn)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	svc := activityService.NewService(activityRepo.NewRepository(db), spaceRepo.NewRepository(db), authorization.NewAuthorizer(), nil)
	repo := &activityRepo.Repository{DB: db}
	wib := time.FixedZone("WIB", 7*3600)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, wib) }
	ip := func(v int) *int { return &v }
	tp := func(v time.Time) *time.Time { return &v }
	sp := func(v string) *string { return &v }

	newSpace := func(t *testing.T, label string) scenarioSpace {
		t.Helper()
		spaceID := utils.CreateUUID()
		ownerUser := utils.CreateUUID()
		if err := db.Exec(`INSERT INTO users (id, name, email, password) VALUES (?, ?, ?, '')`, ownerUser, "Owner "+label, label+"-owner@scenario.example.test").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO spaces (id, name, type, category, created_by_user_id) VALUES (?, ?, 'SHARED', 'family', ?)`, spaceID, "Space "+label, ownerUser).Error; err != nil {
			t.Fatal(err)
		}
		s := scenarioSpace{id: spaceID}
		join := func(role, userID string) scenarioActor {
			if userID == "" {
				userID = utils.CreateUUID()
				if err := db.Exec(`INSERT INTO users (id, name, email, password) VALUES (?, ?, ?, '')`, userID, role+" "+label, label+"-"+role+"@scenario.example.test").Error; err != nil {
					t.Fatal(err)
				}
			}
			memberID := utils.CreateUUID()
			if err := db.Exec(`INSERT INTO space_members (id, space_id, user_id, role_id) SELECT ?, ?, ?, id FROM roles WHERE name = ? AND deleted_at IS NULL`, memberID, spaceID, userID, role).Error; err != nil {
				t.Fatal(err)
			}
			return scenarioActor{memberID: memberID, actor: actorFor(t, db, spaceID, userID, memberID, role)}
		}
		s.owner = join("space_owner", ownerUser)
		s.member = join("space_member", "")
		s.viewer = join("space_viewer", "")
		// A user who is not a member of the Space but holds a space_member role elsewhere.
		outsideUser := utils.CreateUUID()
		if err := db.Exec(`INSERT INTO users (id, name, email, password) VALUES (?, ?, ?, '')`, outsideUser, "Outside "+label, label+"-outside@scenario.example.test").Error; err != nil {
			t.Fatal(err)
		}
		s.outside = scenarioActor{memberID: utils.CreateUUID(), actor: actorFor(t, db, spaceID, outsideUser, "", "space_member")}
		s.outside.actor.MemberID = ""
		return s
	}
	rawRow := func(t *testing.T, id string) (kind, subject, note, details string, updatedAt time.Time) {
		t.Helper()
		if err := db.Raw(`SELECT kind, COALESCE(subject,''), note, details::text, updated_at FROM space_activities WHERE id = ?`, id).Row().Scan(&kind, &subject, &note, &details, &updatedAt); err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		return
	}
	load := func(t *testing.T, id string) domainactivity.Activity {
		t.Helper()
		var a domainactivity.Activity
		if err := db.Unscoped().Where("id = ?", id).First(&a).Error; err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		return a
	}
	createFeeding := func(t *testing.T, s scenarioSpace, who scenarioActor, when time.Time, volume int) *domainactivity.Activity {
		t.Helper()
		a, err := svc.Create(ctx, who.actor, dto.ActivityCreateInput{
			Space: s.id, Kind: "feeding", Subject: "Zeia", Details: map[string]any{"volume_ml": volume}, OccurredAt: when,
		})
		if err != nil {
			t.Fatalf("create feeding: %v", err)
		}
		return a
	}
	type snapshot struct {
		note, details string
		updatedAt     time.Time
	}
	snap := func(t *testing.T, id string) snapshot {
		t.Helper()
		_, _, note, details, upd := rawRow(t, id)
		return snapshot{note, details, upd}
	}
	assertSame := func(t *testing.T, id string, before snapshot) {
		t.Helper()
		after := snap(t, id)
		if after.note != before.note || after.details != before.details || !after.updatedAt.Equal(before.updatedAt) {
			t.Fatalf("row %s changed: before=%+v after=%+v", id, before, after)
		}
	}
	isValidation := func(err error) bool {
		var ve *authorization.ValidationError
		var de *domainactivity.DetailsError
		return errors.As(err, &ve) || errors.As(err, &de)
	}

	t.Run("a: 1 Nov ambiguous report is refused then resolved by id or time", func(t *testing.T) {
		s := newSpace(t, "scn-a")
		early := createFeeding(t, s, s.owner, at(1, 18, 45), 90)
		late := createFeeding(t, s, s.owner, at(1, 21, 30), 120)
		beforeEarly, beforeLate := snap(t, early.ID), snap(t, late.ID)

		_, err := svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, MessageAt: at(1, 21, 40), LeftoverML: ip(20)})
		var te *domainactivity.FeedingTargetError
		if !errors.As(err, &te) || te.Reason != "ambiguous" {
			t.Fatalf("error = %v, want FeedingTargetError ambiguous", err)
		}
		assertSame(t, early.ID, beforeEarly)
		assertSame(t, late.ID, beforeLate)

		res, err := svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingID: late.ID, MessageAt: at(1, 21, 40), LeftoverML: ip(20)})
		if err != nil || !res.Changed || res.Activity.ID != late.ID {
			t.Fatalf("report by id = %+v, %v", res, err)
		}
		var leftover int
		var reported time.Time
		if err := db.Raw(`SELECT (details->>'leftover_ml')::int, (details->>'leftover_reported_at')::timestamptz FROM space_activities WHERE id = ?`, late.ID).Row().Scan(&leftover, &reported); err != nil {
			t.Fatal(err)
		}
		if leftover != 20 || !reported.Equal(at(1, 21, 40)) {
			t.Fatalf("21:30 row leftover=%d reported=%v", leftover, reported)
		}
		assertSame(t, early.ID, beforeEarly)

		// A named feeding time resolves to 21:30 as well (a repeat of the same report changes nothing).
		res, err = svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingTime: tp(at(1, 21, 30)), MessageAt: at(1, 21, 41), LeftoverML: ip(15)})
		if err != nil || res.Activity.ID != late.ID {
			t.Fatalf("report by time = %+v, %v", res, err)
		}
		if err := db.Raw(`SELECT (details->>'leftover_ml')::int FROM space_activities WHERE id = ?`, late.ID).Row().Scan(&leftover); err != nil || leftover != 15 {
			t.Fatalf("leftover after time report = %d, %v", leftover, err)
		}
		assertSame(t, early.ID, beforeEarly)
	})

	t.Run("b: 3 Nov leftover then expired and SQL consumed aggregate", func(t *testing.T) {
		s := newSpace(t, "scn-b")
		feed := createFeeding(t, s, s.owner, at(3, 12, 0), 90)
		if _, err := svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingID: feed.ID, MessageAt: at(3, 12, 40), LeftoverML: ip(70)}); err != nil {
			t.Fatal(err)
		}
		var outcome *string
		var reported time.Time
		if err := db.Raw(`SELECT details->>'outcome', (details->>'leftover_reported_at')::timestamptz FROM space_activities WHERE id = ?`, feed.ID).Row().Scan(&outcome, &reported); err != nil {
			t.Fatal(err)
		}
		if outcome != nil || !reported.Equal(at(3, 12, 40)) {
			t.Fatalf("after leftover: outcome=%v reported=%v, want open at 12:40", outcome, reported)
		}
		if _, err := svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingID: feed.ID, MessageAt: at(3, 14, 0), Outcome: "expired", ExpiredML: ip(40)}); err != nil {
			t.Fatal(err)
		}
		got := load(t, feed.ID)
		if got.Details["outcome"] != "expired" {
			t.Fatalf("details = %v", got.Details)
		}
		if want := domainactivity.FeedingNote("Zeia", got.Details); got.Note != want || !strings.Contains(got.Note, "40") {
			t.Fatalf("note = %q, want %q", got.Note, want)
		}
		if err := db.Raw(`SELECT (details->>'leftover_reported_at')::timestamptz FROM space_activities WHERE id = ?`, feed.ID).Row().Scan(&reported); err != nil || !reported.Equal(at(3, 14, 0)) {
			t.Fatalf("leftover_reported_at = %v, %v; want 14:00", reported, err)
		}
		var consumed int
		if err := db.Raw(`SELECT COALESCE(SUM((details->>'volume_ml')::int - COALESCE((details->>'expired_ml')::int, 0)), 0)
			FROM space_activities WHERE space_id = ? AND kind = 'feeding' AND deleted_at IS NULL
			AND occurred_at >= ? AND occurred_at < ?`, s.id, at(3, 0, 0), at(4, 0, 0)).Row().Scan(&consumed); err != nil {
			t.Fatal(err)
		}
		if consumed != 50 {
			t.Fatalf("consumed = %d, want 50", consumed)
		}
	})

	t.Run("c: stale client feeding with a note and no details", func(t *testing.T) {
		s := newSpace(t, "scn-c")
		a, err := svc.Create(ctx, s.owner.actor, dto.ActivityCreateInput{Space: s.id, Kind: "feeding", Subject: "Zeia", Note: "Zeia minum 90 ml", OccurredAt: at(3, 8, 0)})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		var note, details string
		if err := db.Raw(`SELECT note, details::text FROM space_activities WHERE id = ?`, a.ID).Row().Scan(&note, &details); err != nil {
			t.Fatal(err)
		}
		if note != "Zeia minum 90 ml" || strings.ReplaceAll(details, " ", "") != "{}" {
			t.Fatalf("note=%q details=%s, want kept note and {}", note, details)
		}
	})

	t.Run("d: update cannot set reported keys or turn a leftover record into a feeding", func(t *testing.T) {
		s := newSpace(t, "scn-d")
		feed := createFeeding(t, s, s.owner, at(3, 9, 0), 90)
		before := snap(t, feed.ID)
		_, err := svc.Update(ctx, s.owner.actor, s.id, feed.ID, dto.ActivityUpdateInput{Details: map[string]any{"outcome": "finished"}})
		if !isValidation(err) {
			t.Fatalf("update outcome error = %v, want validation", err)
		}
		assertSame(t, feed.ID, before)

		noteRow := &domainactivity.Activity{
			SpaceID: s.id, CreatedByMemberID: s.owner.memberID, Kind: "note", Subject: "Zeia",
			Details: domainactivity.Details{"volume_ml": 60, "leftover_ml": 10}, Note: "catatan",
			OccurredAt: at(3, 10, 0), CreatedAt: at(3, 10, 0), UpdatedAt: at(3, 10, 0),
		}
		if err := repo.Create(ctx, noteRow); err != nil {
			t.Fatalf("seed note: %v", err)
		}
		beforeNote := snap(t, noteRow.ID)
		_, err = svc.Update(ctx, s.owner.actor, s.id, noteRow.ID, dto.ActivityUpdateInput{Kind: sp("feeding")})
		if !isValidation(err) {
			t.Fatalf("note->feeding error = %v, want validation", err)
		}
		assertSame(t, noteRow.ID, beforeNote)
		if kind, _, _, _, _ := rawRow(t, noteRow.ID); kind != "note" {
			t.Fatalf("kind = %s, want note", kind)
		}
	})

	t.Run("e: create with leftover in one call", func(t *testing.T) {
		s := newSpace(t, "scn-e")
		when := at(3, 15, 0)
		for name, details := range map[string]map[string]any{
			"leftover": {"volume_ml": 60, "leftover_ml": 35},
			"finished": {"volume_ml": 60, "outcome": "finished"},
			"expired":  {"volume_ml": 60, "outcome": "expired", "expired_ml": 30},
		} {
			a, err := svc.Create(ctx, s.owner.actor, dto.ActivityCreateInput{Space: s.id, Kind: "feeding", Subject: "Zeia", Details: details, OccurredAt: when})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			var reported time.Time
			if err := db.Raw(`SELECT (details->>'leftover_reported_at')::timestamptz FROM space_activities WHERE id = ?`, a.ID).Row().Scan(&reported); err != nil || !reported.Equal(when) {
				t.Fatalf("%s: leftover_reported_at = %v, %v; want %v", name, reported, err, when)
			}
		}
		var rows int
		if err := db.Raw(`SELECT COUNT(*) FROM space_activities WHERE space_id = ?`, s.id).Row().Scan(&rows); err != nil || rows != 3 {
			t.Fatalf("rows = %d, %v; want 3", rows, err)
		}
		_, err := svc.Create(ctx, s.owner.actor, dto.ActivityCreateInput{Space: s.id, Kind: "feeding", Subject: "Zeia", Details: map[string]any{"volume_ml": 60, "leftover_ml": 70}, OccurredAt: when})
		if !isValidation(err) {
			t.Fatalf("leftover > volume error = %v, want validation", err)
		}
		if err := db.Raw(`SELECT COUNT(*) FROM space_activities WHERE space_id = ?`, s.id).Row().Scan(&rows); err != nil || rows != 3 {
			t.Fatalf("rows after refusal = %d, %v; want 3", rows, err)
		}
	})

	t.Run("f: authorization", func(t *testing.T) {
		s := newSpace(t, "scn-f")
		other := newSpace(t, "scn-f-other")
		ownerFeed := createFeeding(t, s, s.owner, at(4, 9, 0), 90)
		beforeOwner := snap(t, ownerFeed.ID)
		foreign := createFeeding(t, other, other.owner, at(4, 9, 5), 90)
		beforeForeign := snap(t, foreign.ID)

		report := func(who scenarioActor, id string) error {
			_, err := svc.ReportFeedingLeftover(ctx, who.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingID: id, MessageAt: at(4, 9, 30), LeftoverML: ip(10)})
			return err
		}
		if err := report(s.viewer, ownerFeed.ID); !errors.Is(err, authorization.ErrForbidden) {
			t.Fatalf("viewer error = %v, want forbidden", err)
		}
		if err := report(s.member, ownerFeed.ID); !errors.Is(err, authorization.ErrForbidden) {
			t.Fatalf("member on owner's feeding error = %v, want forbidden", err)
		}
		assertSame(t, ownerFeed.ID, beforeOwner)
		if err := report(s.outside, ownerFeed.ID); !errors.Is(err, authorization.ErrNotFound) {
			t.Fatalf("non-member error = %v, want not found", err)
		}
		assertSame(t, ownerFeed.ID, beforeOwner)
		if err := report(s.owner, foreign.ID); !errors.Is(err, authorization.ErrNotFound) {
			t.Fatalf("other Space's feeding error = %v, want not found", err)
		}
		assertSame(t, foreign.ID, beforeForeign)

		// A member may report on their own feeding.
		own := createFeeding(t, s, s.member, at(4, 10, 0), 80)
		if err := report(s.member, own.ID); err != nil {
			t.Fatalf("member on own feeding: %v", err)
		}
	})

	seedLegacyFeeding := func(t *testing.T, s scenarioSpace, when time.Time, note string) string {
		t.Helper()
		id := utils.CreateUUID()
		// Rows written before structured details: NULL subject, empty details, prose note.
		if err := db.Exec(`INSERT INTO space_activities (id, space_id, created_by_member_id, kind, note, occurred_at, created_at, updated_at)
			VALUES (?, ?, ?, 'feeding', ?, ?, ?, ?)`, id, s.id, s.owner.memberID, note, when, when, when).Error; err != nil {
			t.Fatalf("seed legacy feeding: %v", err)
		}
		return id
	}

	t.Run("h: legacy prose feeding keeps its note through report and updates", func(t *testing.T) {
		s := newSpace(t, "scn-h")
		const prose = "Feeding Baby Zeia — volume 60 ml"

		reported := seedLegacyFeeding(t, s, at(5, 8, 0), prose)
		res, err := svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingID: reported, MessageAt: at(5, 9, 0), LeftoverML: ip(20)})
		if err != nil || !res.Changed {
			t.Fatalf("report legacy = %+v, %v", res, err)
		}
		_, subject, note, details, updatedAt := rawRow(t, reported)
		if note != prose || res.Activity.Note != prose {
			t.Fatalf("note = %q / %q, want prose kept", note, res.Activity.Note)
		}
		if !strings.Contains(details, `"leftover_ml": 20`) || subject != "" || !updatedAt.After(at(5, 8, 0)) {
			t.Fatalf("row after report: subject=%q details=%s updated_at=%v", subject, details, updatedAt)
		}

		updated := seedLegacyFeeding(t, s, at(5, 10, 0), prose)
		got, err := svc.Update(ctx, s.owner.actor, s.id, updated, dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": 60}})
		if err != nil || got.Note != prose {
			t.Fatalf("volume update = %+v, %v; want prose kept", got, err)
		}
		if _, _, note, details, _ := rawRow(t, updated); note != prose || !strings.Contains(details, `"volume_ml": 60`) {
			t.Fatalf("after volume update: note=%q details=%s", note, details)
		}
		later := at(5, 10, 15)
		if got, err = svc.Update(ctx, s.owner.actor, s.id, updated, dto.ActivityUpdateInput{OccurredAt: &later}); err != nil || got.Note != prose {
			t.Fatalf("occurred_at update = %+v, %v; want prose kept", got, err)
		}
		if _, _, note, _, _ := rawRow(t, updated); note != prose {
			t.Fatalf("after occurred_at update: note=%q", note)
		}
		// A later report on that row still keeps the prose note.
		if _, err := svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingID: updated, MessageAt: at(5, 11, 0), Outcome: "finished"}); err != nil {
			t.Fatal(err)
		}
		if _, _, note, _, _ := rawRow(t, updated); note != prose {
			t.Fatalf("after report: note=%q", note)
		}
	})

	t.Run("i: backend-generated feeding note follows report and update", func(t *testing.T) {
		s := newSpace(t, "scn-i")
		feed := createFeeding(t, s, s.owner, at(5, 12, 0), 90)
		if _, err := svc.ReportFeedingLeftover(ctx, s.owner.actor, dto.FeedingLeftoverInput{Space: s.id, FeedingID: feed.ID, MessageAt: at(5, 13, 0), LeftoverML: ip(30)}); err != nil {
			t.Fatal(err)
		}
		got := load(t, feed.ID)
		if want := domainactivity.FeedingNote("Zeia", got.Details); got.Note != want || !strings.Contains(got.Note, "sisa 30 ml") {
			t.Fatalf("note after report = %q, want %q", got.Note, want)
		}
		if _, err := svc.Update(ctx, s.owner.actor, s.id, feed.ID, dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": 120}}); err != nil {
			t.Fatal(err)
		}
		got = load(t, feed.ID)
		if want := domainactivity.FeedingNote("Zeia", got.Details); got.Note != want || !strings.Contains(got.Note, "volume 120 ml") {
			t.Fatalf("note after update = %q, want %q", got.Note, want)
		}
		if _, err := svc.Update(ctx, s.owner.actor, s.id, feed.ID, dto.ActivityUpdateInput{Details: map[string]any{"volume_ml": nil}}); err != nil {
			t.Fatal(err)
		}
		got = load(t, feed.ID)
		if want := domainactivity.FeedingNote("Zeia", got.Details); got.Note != want || strings.Contains(got.Note, "volume") {
			t.Fatalf("note after volume removal = %q, want %q", got.Note, want)
		}
	})

	t.Run("g: kind alias is stored canonically and found by filter", func(t *testing.T) {
		s := newSpace(t, "scn-g")
		a, err := svc.Create(ctx, s.owner.actor, dto.ActivityCreateInput{Space: s.id, Kind: "Bathing", Subject: "Zeia", Note: "mandi", OccurredAt: at(4, 17, 0)})
		if err != nil {
			t.Fatal(err)
		}
		if kind, _, _, _, _ := rawRow(t, a.ID); kind != "bath" {
			t.Fatalf("stored kind = %q, want bath", kind)
		}
		list, err := svc.List(ctx, s.owner.actor, dto.ActivityListInput{Space: s.id, Kind: "bathing"})
		if err != nil || len(list) != 1 || list[0].ID != a.ID {
			t.Fatalf("list bathing = %+v, %v", list, err)
		}
	})
}

// actorFor builds an ActorContext with the permissions the seeded role holds.
func actorFor(t *testing.T, db *gorm.DB, spaceID, userID, memberID, role string) domainidentity.ActorContext {
	t.Helper()
	var perms []string
	if err := db.Raw(`SELECT p.resource || ':' || p.action FROM roles r
		JOIN role_permissions rp ON rp.role_id = r.id
		JOIN permissions p ON p.id = rp.permission_id AND p.deleted_at IS NULL
		WHERE r.name = ? AND r.deleted_at IS NULL`, role).Scan(&perms).Error; err != nil || len(perms) == 0 {
		t.Fatalf("permissions for %s: %v, %v", role, perms, err)
	}
	set := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		set[p] = struct{}{}
	}
	return domainidentity.ActorContext{UserID: userID, SpaceID: spaceID, MemberID: memberID, RoleName: role, Permissions: set, Source: "mcp"}
}
