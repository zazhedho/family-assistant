package integration

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	activityRepo "family-assistant/internal/repositories/activity"
	"family-assistant/utils"

	"gorm.io/gorm"
)

func TestReportFeedingAgainstPostgres(t *testing.T) {
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
	repo := &activityRepo.Repository{DB: db}
	wib := time.FixedZone("WIB", 7*3600)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, wib) }
	ip := func(v int) *int { return &v }
	tp := func(v time.Time) *time.Time { return &v }

	seedFeeding := func(t *testing.T, f replaceFixture, when time.Time, details domainactivity.Details) *domainactivity.Activity {
		t.Helper()
		a := &domainactivity.Activity{
			SpaceID: f.spaceID, CreatedByMemberID: f.memberID, Kind: "feeding", Subject: "Zeia",
			Details: details, Note: domainactivity.FeedingNote("Zeia", details), OccurredAt: when,
			CreatedAt: when, UpdatedAt: when,
		}
		if err := repo.Create(ctx, a); err != nil {
			t.Fatalf("seed feeding: %v", err)
		}
		return a
	}
	load := func(t *testing.T, id string) domainactivity.Activity {
		t.Helper()
		var a domainactivity.Activity
		if err := db.Unscoped().Where("id = ?", id).First(&a).Error; err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		return a
	}
	leftover := func(a domainactivity.Activity) (float64, bool) {
		v, ok := a.Details["leftover_ml"].(float64)
		return v, ok
	}
	assertUntouched := func(t *testing.T, before *domainactivity.Activity) {
		t.Helper()
		after := load(t, before.ID)
		if _, ok := after.Details["leftover_ml"]; ok || after.Note != before.Note || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Fatalf("feeding %s changed: details=%v note=%q updated_at=%v", before.ID, after.Details, after.Note, after.UpdatedAt)
		}
	}
	targetErr := func(t *testing.T, err error, reason string) *domainactivity.FeedingTargetError {
		t.Helper()
		var te *domainactivity.FeedingTargetError
		if !errors.As(err, &te) || te.Reason != reason {
			t.Fatalf("error = %v, want feeding target %s", err, reason)
		}
		return te
	}

	t.Run("clear case attaches leftover to the one open feeding and regenerates the note", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-clear")
		closed := seedFeeding(t, f, at(5, 10, 0), domainactivity.Details{"volume_ml": 90, "outcome": "finished", "expired_ml": 0})
		open := seedFeeding(t, f, at(5, 12, 0), domainactivity.Details{"volume_ml": 90})
		now := at(5, 13, 1)
		res, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{MessageAt: at(5, 13, 0)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(30)}, ReportedAt: at(5, 13, 0), Now: now,
		})
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		if !res.Changed || res.Activity.ID != open.ID {
			t.Fatalf("result = %+v, want changed on %s", res, open.ID)
		}
		if _, had := res.Previous["leftover_ml"]; had || res.Previous["volume_ml"] == nil {
			t.Fatalf("previous = %v, want the details before the report", res.Previous)
		}
		got := load(t, open.ID)
		if v, ok := leftover(got); !ok || v != 30 {
			t.Fatalf("leftover = %v, want 30", got.Details)
		}
		if want := "Feeding Zeia — volume 90 ml; sisa 30 ml"; got.Note != want || res.Activity.Note != want {
			t.Fatalf("note = %q / %q, want %q", got.Note, res.Activity.Note, want)
		}
		if !got.UpdatedAt.Equal(now) {
			t.Fatalf("updated_at = %v, want %v", got.UpdatedAt, now)
		}
		if after := load(t, closed.ID); after.Note != closed.Note || !after.UpdatedAt.Equal(closed.UpdatedAt) {
			t.Fatalf("closed feeding touched: %+v", after)
		}

		again, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{FeedingID: open.ID, MessageAt: at(5, 13, 5)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(30)}, ReportedAt: at(5, 13, 5), Now: at(5, 13, 6),
		})
		if err != nil || again.Changed {
			t.Fatalf("repeat report = %+v, %v; want unchanged", again, err)
		}
		if after := load(t, open.ID); !after.UpdatedAt.Equal(now) {
			t.Fatalf("unchanged report wrote updated_at %v", after.UpdatedAt)
		}
	})

	t.Run("1 Nov: two open feedings without a time hint are ambiguous; a time hint picks one", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-1nov")
		// With the 3h open lookback, 18:26 and 21:30 cannot both be default targets,
		// so the ambiguous half uses an earlier feeding still inside the window.
		early := seedFeeding(t, f, at(1, 18, 45), domainactivity.Details{"volume_ml": 90})
		late := seedFeeding(t, f, at(1, 21, 30), domainactivity.Details{"volume_ml": 120})
		_, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{MessageAt: at(1, 21, 40)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(20)}, ReportedAt: at(1, 21, 40), Now: at(1, 21, 40),
		})
		te := targetErr(t, err, "ambiguous")
		if len(te.Candidates) != 2 {
			t.Fatalf("candidates = %+v", te.Candidates)
		}
		assertUntouched(t, early)
		assertUntouched(t, late)

		res, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{FeedingTime: tp(at(1, 21, 30)), MessageAt: at(1, 21, 41)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(20)}, ReportedAt: at(1, 21, 41), Now: at(1, 21, 41),
		})
		if err != nil || !res.Changed || res.Activity.ID != late.ID {
			t.Fatalf("report 21:30 = %+v, %v", res, err)
		}
		if v, ok := leftover(load(t, late.ID)); !ok || v != 20 {
			t.Fatal("21:30 feeding should hold leftover 20")
		}
		assertUntouched(t, early)
	})

	t.Run("1 Nov literal: 18:26 report with a time hint never lands on 21:30", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-1nov-literal")
		early := seedFeeding(t, f, at(1, 18, 26), domainactivity.Details{"volume_ml": 90})
		late := seedFeeding(t, f, at(1, 21, 30), domainactivity.Details{"volume_ml": 120})
		res, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{FeedingTime: tp(at(1, 18, 30)), MessageAt: at(1, 21, 45)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(15)}, ReportedAt: at(1, 21, 45), Now: at(1, 21, 45),
		})
		if err != nil || res.Activity.ID != early.ID {
			t.Fatalf("report 18:26 = %+v, %v", res, err)
		}
		assertUntouched(t, late)
	})

	t.Run("3 Nov: leftover on feeding B never copies from feeding A", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-3nov")
		a := seedFeeding(t, f, at(3, 9, 0), domainactivity.Details{"volume_ml": 90, "leftover_ml": 20, "leftover_reported_at": at(3, 10, 0).Format(time.RFC3339)})
		b := seedFeeding(t, f, at(3, 12, 0), domainactivity.Details{"volume_ml": 100})
		res, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{FeedingTime: tp(at(3, 12, 0)), MessageAt: at(3, 13, 0)},
			Report: domainactivity.FeedingReport{Outcome: domainactivity.OutcomeFinished}, ReportedAt: at(3, 13, 0), Now: at(3, 13, 0),
		})
		if err != nil || res.Activity.ID != b.ID {
			t.Fatalf("report B = %+v, %v", res, err)
		}
		gotB := load(t, b.ID)
		if _, ok := gotB.Details["leftover_ml"]; ok {
			t.Fatalf("feeding B gained a leftover it was never given: %v", gotB.Details)
		}
		if gotB.Details["outcome"] != domainactivity.OutcomeFinished {
			t.Fatalf("feeding B details = %v", gotB.Details)
		}
		gotA := load(t, a.ID)
		if v, ok := leftover(gotA); !ok || v != 20 || gotA.Details["outcome"] != nil {
			t.Fatalf("feeding A changed: %v", gotA.Details)
		}

		res, err = repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{FeedingID: b.ID, MessageAt: at(3, 13, 5)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(45)}, ReportedAt: at(3, 13, 5), Now: at(3, 13, 5),
		})
		if err != nil || !res.Changed {
			t.Fatalf("leftover B = %+v, %v", res, err)
		}
		if v, _ := leftover(load(t, b.ID)); v != 45 {
			t.Fatal("feeding B leftover should be 45")
		}
		if v, _ := leftover(load(t, a.ID)); v != 20 {
			t.Fatal("feeding A leftover should stay 20")
		}
	})

	t.Run("soft-deleted, other-kind and other-Space feedings are excluded", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-excl")
		other := seedReplaceFixture(t, db, "feed-excl-other")
		deleted := seedFeeding(t, f, at(5, 12, 0), domainactivity.Details{"volume_ml": 90})
		if err := repo.SoftDelete(ctx, f.spaceID, deleted.ID, at(5, 12, 5)); err != nil {
			t.Fatal(err)
		}
		foreign := seedFeeding(t, other, at(5, 12, 10), domainactivity.Details{"volume_ml": 90})
		diaper := &domainactivity.Activity{SpaceID: f.spaceID, CreatedByMemberID: f.memberID, Kind: "diaper", OccurredAt: at(5, 12, 20)}
		if err := repo.Create(ctx, diaper); err != nil {
			t.Fatal(err)
		}
		_, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{MessageAt: at(5, 13, 0)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(10)}, ReportedAt: at(5, 13, 0), Now: at(5, 13, 0),
		})
		te := targetErr(t, err, "none")
		if len(te.Candidates) != 0 {
			t.Fatalf("candidates = %+v, want none", te.Candidates)
		}
		_, err = repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{FeedingID: deleted.ID, MessageAt: at(5, 13, 0)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(10)}, ReportedAt: at(5, 13, 0), Now: at(5, 13, 0),
		})
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("deleted feeding by id error = %v, want not found", err)
		}
		if after := load(t, foreign.ID); !after.UpdatedAt.Equal(foreign.UpdatedAt) {
			t.Fatal("foreign feeding touched")
		}
	})

	t.Run("none lists the most recent feedings", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-none")
		for h := 1; h <= 7; h++ {
			seedFeeding(t, f, at(4, h, 0), domainactivity.Details{"volume_ml": 60, "outcome": "finished", "expired_ml": 0})
		}
		_, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{MessageAt: at(4, 20, 0)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(10)}, ReportedAt: at(4, 20, 0), Now: at(4, 20, 0),
		})
		te := targetErr(t, err, "none")
		if len(te.Candidates) != 5 || !te.Candidates[0].OccurredAt.Equal(at(4, 7, 0)) {
			t.Fatalf("candidates = %+v, want the 5 most recent", te.Candidates)
		}
	})

	t.Run("cross-Space feeding id returns not found", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-xspace")
		other := seedReplaceFixture(t, db, "feed-xspace-other")
		foreign := seedFeeding(t, other, at(5, 12, 0), domainactivity.Details{"volume_ml": 90})
		for _, id := range []string{foreign.ID, utils.CreateUUID(), "not-a-uuid"} {
			_, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
				SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{FeedingID: id, MessageAt: at(5, 13, 0)},
				Report: domainactivity.FeedingReport{LeftoverML: ip(10)}, ReportedAt: at(5, 13, 0), Now: at(5, 13, 0),
			})
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("id %q error = %v, want not found", id, err)
			}
		}
		assertUntouched(t, foreign)
	})

	t.Run("CanMutate false writes nothing", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-forbid")
		open := seedFeeding(t, f, at(5, 12, 0), domainactivity.Details{"volume_ml": 90})
		var seen string
		_, err := repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
			SpaceID: f.spaceID, Selector: domainactivity.FeedingSelector{MessageAt: at(5, 13, 0)},
			Report: domainactivity.FeedingReport{LeftoverML: ip(10)}, ReportedAt: at(5, 13, 0), Now: at(5, 13, 0),
			CanMutate: func(a *domainactivity.Activity) bool { seen = a.ID; return false },
		})
		if !errors.Is(err, domainactivity.ErrFeedingForbidden) {
			t.Fatalf("error = %v, want forbidden", err)
		}
		if seen != open.ID {
			t.Fatalf("CanMutate saw %q, want %q", seen, open.ID)
		}
		assertUntouched(t, open)
	})

	t.Run("concurrent reports stay consistent", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "feed-race")
		a := seedFeeding(t, f, at(5, 9, 0), domainactivity.Details{"volume_ml": 90})
		b := seedFeeding(t, f, at(5, 11, 0), domainactivity.Details{"volume_ml": 120})
		report := func(sel domainactivity.FeedingSelector, ml int) (*domainactivity.FeedingReportResult, error) {
			return repo.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
				SpaceID: f.spaceID, Selector: sel, Report: domainactivity.FeedingReport{LeftoverML: ip(ml)},
				ReportedAt: sel.MessageAt, Now: sel.MessageAt,
			})
		}
		for round := 0; round < 10; round++ {
			if err := db.Exec(`UPDATE space_activities SET details = '{"volume_ml":90}' WHERE id = ?`, a.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`UPDATE space_activities SET details = '{"volume_ml":120}' WHERE id = ?`, b.ID).Error; err != nil {
				t.Fatal(err)
			}

			// Same feeding, same report: exactly one writer sees a change.
			var wg sync.WaitGroup
			results := make([]*domainactivity.FeedingReportResult, 2)
			errs := make([]error, 2)
			for i := range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results[i], errs[i] = report(domainactivity.FeedingSelector{FeedingID: a.ID, MessageAt: at(5, 12, 0)}, 25)
				}()
			}
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					t.Fatalf("round %d same feeding: %v", round, err)
				}
			}
			if results[0].Changed == results[1].Changed {
				t.Fatalf("round %d: changed = %v/%v, want exactly one", round, results[0].Changed, results[1].Changed)
			}
			gotA := load(t, a.ID)
			if v, _ := leftover(gotA); v != 25 || gotA.Note != domainactivity.FeedingNote("Zeia", gotA.Details) {
				t.Fatalf("round %d: feeding A = %v %q", round, gotA.Details, gotA.Note)
			}

			// Different feedings in parallel: each report lands on its own feeding.
			wg = sync.WaitGroup{}
			sels := []struct {
				sel domainactivity.FeedingSelector
				ml  int
			}{
				{domainactivity.FeedingSelector{FeedingTime: tp(at(5, 9, 5)), MessageAt: at(5, 12, 5)}, 40},
				{domainactivity.FeedingSelector{FeedingTime: tp(at(5, 11, 5)), MessageAt: at(5, 12, 5)}, 70},
			}
			for i := range sels {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results[i], errs[i] = report(sels[i].sel, sels[i].ml)
				}()
			}
			wg.Wait()
			for i, err := range errs {
				if err != nil {
					t.Fatalf("round %d different feedings: %v", round, err)
				}
				want := []string{a.ID, b.ID}[i]
				if results[i].Activity.ID != want {
					t.Fatalf("round %d: report %d landed on %s, want %s", round, i, results[i].Activity.ID, want)
				}
			}
			gotA, gotB := load(t, a.ID), load(t, b.ID)
			if va, _ := leftover(gotA); va != 40 {
				t.Fatalf("round %d: A details = %v", round, gotA.Details)
			}
			if vb, _ := leftover(gotB); vb != 70 {
				t.Fatalf("round %d: B details = %v", round, gotB.Details)
			}
			if gotA.Note != domainactivity.FeedingNote("Zeia", gotA.Details) || gotB.Note != domainactivity.FeedingNote("Zeia", gotB.Details) {
				t.Fatalf("round %d: notes out of sync: %q %q", round, gotA.Note, gotB.Note)
			}
		}
	})
}

func TestActivityUpdateDetailsPatchAgainstPostgres(t *testing.T) {
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
	repo := &activityRepo.Repository{DB: db}
	f := seedReplaceFixture(t, db, "details-patch")
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	a := &domainactivity.Activity{
		SpaceID: f.spaceID, CreatedByMemberID: f.memberID, Kind: "feeding", OccurredAt: at,
		Details: domainactivity.Details{"volume_ml": 90, "leftover_ml": 20, `we"ird\key`: "x"},
	}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}

	subject := " Zeia "
	err = repo.Update(ctx, f.spaceID, a.ID, domainactivity.UpdateFields{
		Subject:      &subject,
		DetailsPatch: domainactivity.Details{"volume_ml": 120, "leftover_ml": nil, `we"ird\key`: nil},
	}, at.Add(time.Minute))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.FindByIDInSpace(ctx, f.spaceID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Zeia" || len(got.Details) != 1 || got.Details["volume_ml"] != float64(120) {
		t.Fatalf("after patch: subject=%q details=%v", got.Subject, got.Details)
	}

	// Concurrent patches to different keys both survive.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, patch := range []domainactivity.Details{{"leftover_ml": 30}, {"outcome": "expired", "expired_ml": 10}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = repo.Update(ctx, f.spaceID, a.ID, domainactivity.UpdateFields{DetailsPatch: patch}, at.Add(2*time.Minute))
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent patch: %v", err)
		}
	}
	got, err = repo.FindByIDInSpace(ctx, f.spaceID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Details["leftover_ml"] != float64(30) || got.Details["outcome"] != "expired" || got.Details["expired_ml"] != float64(10) {
		t.Fatalf("concurrent patches lost a key: %v", got.Details)
	}

	other := seedReplaceFixture(t, db, "details-patch-other")
	if err := repo.Update(ctx, other.spaceID, a.ID, domainactivity.UpdateFields{DetailsPatch: domainactivity.Details{"x": 1}}, at); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-Space update error = %v, want not found", err)
	}
}
