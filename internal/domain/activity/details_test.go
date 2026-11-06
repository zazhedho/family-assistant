package domainactivity

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeKind(t *testing.T) {
	cases := map[string]string{
		" Bathing ":          "bath",
		"bottle feeding":     "feeding",
		"Bowel_Movement":     "bowel_movement",
		"growth measurement": "growth_measurement",
		"":                   "",
	}
	for in, want := range cases {
		if got := NormalizeKind(in); got != want {
			t.Errorf("NormalizeKind(%q)=%q want %q", in, got, want)
		}
	}
}

func TestValidateDetails(t *testing.T) {
	big := Details{"x": strings.Repeat("a", 4100)}
	cases := []struct {
		name    string
		kind    string
		details Details
		wantErr bool
	}{
		{"feeding volume", "feeding", Details{"volume_ml": 60}, false},
		{"feeding float volume", "feeding", Details{"volume_ml": float64(60)}, false},
		{"feeding empty", "feeding", Details{}, false},
		{"feeding nil", "feeding", nil, false},
		{"feeding zero", "feeding", Details{"volume_ml": 0}, true},
		{"feeding negative", "feeding", Details{"volume_ml": -5}, true},
		{"feeding string", "feeding", Details{"volume_ml": "60"}, true},
		{"feeding fraction", "feeding", Details{"volume_ml": 60.5}, true},
		{"feeding all keys", "feeding", Details{"volume_ml": 100, "leftover_ml": 20, "leftover_reported_at": "2026-11-05T10:00:00+07:00", "outcome": "expired", "expired_ml": 10}, false},
		{"feeding bad time", "feeding", Details{"leftover_reported_at": "yesterday"}, true},
		{"feeding bad outcome", "feeding", Details{"outcome": "spilled"}, true},
		{"feeding leftover too big", "feeding", Details{"volume_ml": 60, "leftover_ml": 70}, true},
		{"feeding expired too big", "feeding", Details{"volume_ml": 60, "expired_ml": 70}, true},
		{"feeding expired needs amount", "feeding", Details{"outcome": "expired"}, true},
		{"feeding negative leftover", "feeding", Details{"leftover_ml": -1}, true},
		{"medication ok", "medication", Details{"name": "Paracetamol"}, false},
		{"medication missing", "medication", Details{}, true},
		{"medication blank", "medication", Details{"name": "  "}, true},
		{"vaccination ok", "vaccination", Details{"vaccine": "BCG"}, false},
		{"vaccination missing", "vaccination", Details{}, true},
		{"temperature ok", "temperature", Details{"celsius": 37.2}, false},
		{"temperature low", "temperature", Details{"celsius": 29.9}, true},
		{"temperature high", "temperature", Details{"celsius": 45.1}, true},
		{"temperature missing", "temperature", Details{}, true},
		{"growth subset", "growth_measurement", Details{"weight_kg": 4.2, "head_cm": 35}, false},
		{"growth zero", "growth_measurement", Details{"weight_kg": 0}, true},
		{"growth string", "growth_measurement", Details{"height_cm": "50"}, true},
		{"unknown any", "mood", Details{"anything": []any{1, "a"}}, false},
		{"feeding too big", "feeding", big, true},
		{"medication too big", "medication", Details{"name": "x", "x": strings.Repeat("a", 4100)}, true},
		{"unknown too big", "mood", big, true},
		{"just over go cap", "mood", Details{"x": strings.Repeat("a", 2700)}, true},
		{"feeding volume at cap", "feeding", Details{"volume_ml": MaxFeedingML}, false},
		{"feeding volume over cap", "feeding", Details{"volume_ml": MaxFeedingML + 1}, true},
		{"feeding huge native leftover", "feeding", Details{"leftover_ml": 9_000_000_000_000_000_000}, true},
		{"feeding huge native expired", "feeding", Details{"expired_ml": int64(9_000_000_000_000_000_000), "outcome": "expired"}, true},
		{"unknown huge number", "mood", Details{"a": 1e300, "b": "x"}, true},
		{"unknown huge nested number", "mood", Details{"a": map[string]any{"b": []any{1, -1e16}}}, true},
		{"unknown huge native int", "mood", Details{"a": int64(1) << 60}, true},
		{"unknown number at bound", "mood", Details{"a": 1e15, "b": []any{-1e15}}, false},
		{"medication huge extra number", "medication", Details{"name": "x", "dose": 1e300}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDetails(tc.kind, tc.details)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if err != nil {
				var de *DetailsError
				if !errors.As(err, &de) {
					t.Fatalf("want *DetailsError, got %T", err)
				}
			}
		})
	}
}

func TestApplyDetailsPatch(t *testing.T) {
	base := Details{"a": 1, "b": 2}
	patch := Details{"b": nil, "c": 3}
	got := ApplyDetailsPatch(base, patch)
	want := Details{"a": 1, "c": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if !reflect.DeepEqual(base, Details{"a": 1, "b": 2}) || len(patch) != 2 {
		t.Fatal("inputs mutated")
	}
	if ApplyDetailsPatch(nil, nil) == nil {
		t.Fatal("want non-nil map")
	}
}

func TestDetailsScanValue(t *testing.T) {
	var d Details
	if err := d.Scan(nil); err != nil || d == nil || len(d) != 0 {
		t.Fatalf("nil scan: %v %v", d, err)
	}
	if err := d.Scan([]byte(`{"volume_ml":60}`)); err != nil || d["volume_ml"] != float64(60) {
		t.Fatalf("bytes scan: %v %v", d, err)
	}
	if err := d.Scan(""); err != nil || len(d) != 0 {
		t.Fatalf("empty scan: %v %v", d, err)
	}
	if err := d.Scan(42); err == nil {
		t.Fatal("want error for int")
	}
	v, err := Details{"a": 1}.Value()
	if err != nil || v != `{"a":1}` {
		t.Fatalf("value: %v %v", v, err)
	}
	v, _ = Details(nil).Value()
	if v != "{}" {
		t.Fatalf("nil value: %v", v)
	}
}
