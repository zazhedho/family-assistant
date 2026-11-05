package domainactivity

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFeedingNote(t *testing.T) {
	n := FeedingNote("Zeia", Details{"volume_ml": 60})
	if !strings.Contains(n, "Feeding Zeia") || !strings.Contains(n, "60 ml") {
		t.Fatalf("note=%q", n)
	}
	if got := FeedingNote("Zeia", Details{"volume_ml": 60, "leftover_ml": 20}); !strings.Contains(got, "sisa 20 ml") {
		t.Fatalf("leftover: %q", got)
	}
	if got := FeedingNote("Zeia", Details{"volume_ml": 60, "outcome": "finished"}); !strings.Contains(got, "habis diminum") {
		t.Fatalf("finished: %q", got)
	}
	got := FeedingNote("Zeia", Details{"volume_ml": 100, "leftover_ml": float64(70), "outcome": "expired", "expired_ml": 40})
	if !strings.Contains(got, "expired") || !strings.Contains(got, "40 ml") {
		t.Fatalf("expired: %q", got)
	}
	d := Details{"volume_ml": 100, "leftover_ml": 70, "outcome": "expired", "expired_ml": 40, "junk": "free text"}
	first, second := FeedingNote("Zeia", d), FeedingNote("Zeia", d)
	if first != second {
		t.Fatal("not deterministic")
	}
	if strings.Contains(FeedingNote("Zeia", d), "free text") {
		t.Fatal("leaks free text")
	}
	if e := FeedingNote("", Details{"volume_ml": 60}); strings.Contains(e, "  ") || !strings.HasPrefix(e, "Feeding") {
		t.Fatalf("empty subject: %q", e)
	}
}

var base = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

func feed(id string, at time.Time, d Details) Activity {
	return Activity{ID: id, Kind: "feeding", OccurredAt: at, Details: d}
}

func targetErr(t *testing.T, err error, reason string, n int) *FeedingTargetError {
	t.Helper()
	var te *FeedingTargetError
	if !errors.As(err, &te) {
		t.Fatalf("want FeedingTargetError, got %v", err)
	}
	if te.Reason != reason || len(te.Candidates) != n {
		t.Fatalf("reason=%s candidates=%d, want %s/%d", te.Reason, len(te.Candidates), reason, n)
	}
	return te
}

func TestSelectFeeding(t *testing.T) {
	at := func(h, m int) time.Time { return base.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	closed := Details{"outcome": "finished"}
	tp := func(x time.Time) *time.Time { return &x }

	t.Run("by id closed", func(t *testing.T) {
		fs := []Activity{feed("a", at(10, 0), closed), feed("b", at(12, 0), nil)}
		got, err := SelectFeeding(fs, FeedingSelector{FeedingID: "a", MessageAt: at(13, 0)})
		if err != nil || got.ID != "a" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("by id missing", func(t *testing.T) {
		_, err := SelectFeeding([]Activity{feed("a", at(10, 0), nil)}, FeedingSelector{FeedingID: "zz", MessageAt: at(13, 0)})
		_ = targetErr(t, err, "none", 1)
	})
	t.Run("by time single", func(t *testing.T) {
		fs := []Activity{feed("a", at(10, 0), nil), feed("b", at(12, 0), nil)}
		got, err := SelectFeeding(fs, FeedingSelector{FeedingTime: tp(at(10, 15)), MessageAt: at(13, 0)})
		if err != nil || got.ID != "a" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("by time ambiguous", func(t *testing.T) {
		fs := []Activity{feed("a", at(10, 0), nil), feed("b", at(10, 30), nil)}
		_, err := SelectFeeding(fs, FeedingSelector{FeedingTime: tp(at(10, 15)), MessageAt: at(13, 0)})
		_ = targetErr(t, err, "ambiguous", 2)
	})
	t.Run("by time none", func(t *testing.T) {
		fs := []Activity{feed("a", at(10, 0), nil), feed("b", at(12, 0), nil)}
		_, err := SelectFeeding(fs, FeedingSelector{FeedingTime: tp(at(8, 0)), MessageAt: at(13, 0)})
		_ = targetErr(t, err, "none", 2)
	})
	t.Run("by time outside open window", func(t *testing.T) {
		fs := []Activity{feed("a", at(10, 0), closed)}
		got, err := SelectFeeding(fs, FeedingSelector{FeedingTime: tp(at(10, 5)), MessageAt: at(14, 0)})
		if err != nil || got.ID != "a" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("default single open", func(t *testing.T) {
		fs := []Activity{feed("a", at(18, 26), nil)}
		got, err := SelectFeeding(fs, FeedingSelector{MessageAt: at(19, 0)})
		if err != nil || got.ID != "a" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("default two open ambiguous", func(t *testing.T) {
		fs := []Activity{feed("a", at(19, 30), nil), feed("b", at(21, 30), nil)}
		_, err := SelectFeeding(fs, FeedingSelector{MessageAt: at(21, 40)})
		te := targetErr(t, err, "ambiguous", 2)
		if !te.Candidates[0].Open {
			t.Fatal("candidate should be open")
		}
	})
	t.Run("open feeding beyond 3h is not auto-selected", func(t *testing.T) {
		fs := []Activity{feed("a", at(18, 26), nil)}
		te := targetErr(t, func() error { _, err := SelectFeeding(fs, FeedingSelector{MessageAt: at(21, 40)}); return err }(), "none", 1)
		if te.Candidates[0].ID != "a" {
			t.Fatal("candidate should list a")
		}
		got, err := SelectFeeding(fs, FeedingSelector{FeedingTime: tp(at(18, 30)), MessageAt: at(21, 40)})
		if err != nil || got.ID != "a" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("default skips closed", func(t *testing.T) {
		fs := []Activity{feed("a", at(18, 26), closed), feed("b", at(21, 30), nil)}
		got, err := SelectFeeding(fs, FeedingSelector{MessageAt: at(21, 40)})
		if err != nil || got.ID != "b" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("default skips future and old", func(t *testing.T) {
		fs := []Activity{feed("old", at(10, 0), nil), feed("future", at(22, 0), nil)}
		_, err := SelectFeeding(fs, FeedingSelector{MessageAt: at(21, 40)})
		_ = targetErr(t, err, "none", 2)
	})
}

func ip(i int) *int { return &i }

func TestApplyFeedingReport(t *testing.T) {
	rep := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	stamp := rep.Format(time.RFC3339)

	t.Run("leftover only", func(t *testing.T) {
		got, changed, err := ApplyFeedingReport(Details{"volume_ml": 100}, FeedingReport{LeftoverML: ip(70)}, rep)
		if err != nil || !changed {
			t.Fatal(err, changed)
		}
		if got["leftover_ml"] != 70 || got["leftover_reported_at"] != stamp {
			t.Fatalf("%v", got)
		}
		if _, ok := got["outcome"]; ok {
			t.Fatal("must stay open")
		}
	})
	t.Run("finished", func(t *testing.T) {
		got, changed, err := ApplyFeedingReport(Details{"volume_ml": 100}, FeedingReport{Outcome: "finished"}, rep)
		if err != nil || !changed || got["outcome"] != "finished" || got["expired_ml"] != 0 {
			t.Fatalf("%v %v %v", got, changed, err)
		}
	})
	t.Run("expired without amount", func(t *testing.T) {
		if _, _, err := ApplyFeedingReport(Details{"volume_ml": 100}, FeedingReport{Outcome: "expired"}, rep); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("expired with amount", func(t *testing.T) {
		got, _, err := ApplyFeedingReport(Details{"volume_ml": 100},
			FeedingReport{LeftoverML: ip(70), Outcome: "expired", ExpiredML: ip(40)}, rep)
		if err != nil || got["expired_ml"] != 40 || got["outcome"] != "expired" || got["leftover_ml"] != 70 {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("expired amount from stored", func(t *testing.T) {
		got, _, err := ApplyFeedingReport(Details{"volume_ml": 100, "expired_ml": float64(30)}, FeedingReport{Outcome: "expired"}, rep)
		if err != nil || got["expired_ml"] != 30 {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("stored zero does not satisfy expired", func(t *testing.T) {
		cur := Details{"volume_ml": 100, "outcome": "finished", "expired_ml": 0}
		if _, _, err := ApplyFeedingReport(cur, FeedingReport{Outcome: "expired"}, rep); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("leftover above volume", func(t *testing.T) {
		if _, _, err := ApplyFeedingReport(Details{"volume_ml": 60}, FeedingReport{LeftoverML: ip(70)}, rep); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("leftover without volume", func(t *testing.T) {
		if _, _, err := ApplyFeedingReport(Details{}, FeedingReport{LeftoverML: ip(70)}, rep); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("identical repeat", func(t *testing.T) {
		cur := Details{"volume_ml": 100, "leftover_ml": float64(70), "leftover_reported_at": "2026-11-05T08:00:00Z"}
		got, changed, err := ApplyFeedingReport(cur, FeedingReport{LeftoverML: ip(70)}, rep)
		if err != nil || changed || got["leftover_reported_at"] != "2026-11-05T08:00:00Z" {
			t.Fatalf("%v %v %v", got, changed, err)
		}
	})
	t.Run("overwrite closed", func(t *testing.T) {
		cur := Details{"volume_ml": 100, "outcome": "finished", "expired_ml": 0}
		got, changed, err := ApplyFeedingReport(cur, FeedingReport{Outcome: "expired", ExpiredML: ip(20)}, rep)
		if err != nil || !changed || got["outcome"] != "expired" || got["leftover_reported_at"] != stamp {
			t.Fatalf("%v %v %v", got, changed, err)
		}
	})
	t.Run("nothing reported", func(t *testing.T) {
		if _, _, err := ApplyFeedingReport(Details{}, FeedingReport{}, rep); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("does not mutate input", func(t *testing.T) {
		cur := Details{"volume_ml": 100}
		_, _, _ = ApplyFeedingReport(cur, FeedingReport{LeftoverML: ip(10)}, rep)
		if len(cur) != 1 {
			t.Fatal("mutated")
		}
	})
}

func TestFeedingReportedKeys(t *testing.T) {
	if len(FeedingReportedKeys()) != 4 {
		t.Fatal("want 4 keys")
	}
}

func TestIsGeneratedFeedingNote(t *testing.T) {
	d := Details{"volume_ml": float64(60), "leftover_ml": float64(20)}
	if !IsGeneratedFeedingNote("Zeia", d, FeedingNote("Zeia", d)) {
		t.Fatal("backend note not recognised")
	}
	if !IsGeneratedFeedingNote("", Details{"volume_ml": 60}, FeedingNote("", Details{"volume_ml": 60})) {
		t.Fatal("backend note without subject not recognised")
	}
	for name, tc := range map[string]struct {
		subject string
		details Details
		note    string
	}{
		"legacy prose":    {"", Details{}, "Feeding Baby Zeia — volume 60 ml"},
		"caller note":     {"Zeia", Details{"volume_ml": 60}, "Zeia minum 60 ml"},
		"stale generated": {"Zeia", Details{"volume_ml": 90}, FeedingNote("Zeia", Details{"volume_ml": 60})},
		"other subject":   {"Bima", Details{"volume_ml": 60}, FeedingNote("Zeia", Details{"volume_ml": 60})},
		"empty note":      {"", Details{}, ""},
	} {
		if IsGeneratedFeedingNote(tc.subject, tc.details, tc.note) {
			t.Fatalf("%s: treated as generated", name)
		}
	}
}

func TestFeedingReportOverwrites(t *testing.T) {
	stamp := "2026-11-05T10:00:00Z"
	for _, tc := range []struct {
		name           string
		previous, next Details
		want           bool
	}{
		{"first leftover", Details{"volume_ml": 100}, Details{"volume_ml": 100, "leftover_ml": 40, "leftover_reported_at": stamp}, false},
		{"leftover then finished", Details{"volume_ml": 100, "leftover_ml": float64(40)}, Details{"volume_ml": 100, "leftover_ml": 40, "outcome": "finished", "expired_ml": 0}, false},
		{"leftover 40 then 30", Details{"volume_ml": 100, "leftover_ml": float64(40)}, Details{"volume_ml": 100, "leftover_ml": 30}, true},
		{"expired_ml only", Details{"volume_ml": 100, "outcome": "expired", "expired_ml": float64(20)}, Details{"volume_ml": 100, "outcome": "expired", "expired_ml": 30}, true},
		{"outcome changed", Details{"volume_ml": 100, "outcome": "finished", "expired_ml": float64(0)}, Details{"volume_ml": 100, "outcome": "expired", "expired_ml": 0}, true},
		{"reopened", Details{"volume_ml": 100, "leftover_ml": float64(0), "outcome": "finished", "expired_ml": float64(0)}, Details{"volume_ml": 100, "leftover_ml": 30}, true},
		{"identical", Details{"volume_ml": 100, "leftover_ml": float64(40)}, Details{"volume_ml": 100, "leftover_ml": 40}, false},
		{"only timestamp differs", Details{"leftover_ml": 40, "leftover_reported_at": "2026-11-05T08:00:00Z"}, Details{"leftover_ml": 40, "leftover_reported_at": stamp}, false},
	} {
		if got := FeedingReportOverwrites(tc.previous, tc.next); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
