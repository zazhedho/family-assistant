package domainactivity

import (
	"errors"
	"testing"
)

func TestRegisteredKindsValidate(t *testing.T) {
	cases := []struct {
		name      string
		kind      string
		details   Details
		wantField string // empty means valid
	}{
		{"feeding ok", "feeding", Details{"volume_ml": 60}, ""},
		{"feeding alias ok", "Bottle Feeding", Details{"volume_ml": 60}, ""},
		{"feeding zero volume", "feeding", Details{"volume_ml": 0}, "details.volume_ml"},
		{"feeding leftover too big", "feeding", Details{"volume_ml": 60, "leftover_ml": 70}, "details.leftover_ml"},
		{"feeding bad outcome", "feeding", Details{"outcome": "spilled"}, "details.outcome"},
		{"medication ok", "medication", Details{"name": "Paracetamol"}, ""},
		{"medication missing", "medication", Details{}, "details.name"},
		{"vaccination ok", "vaccination", Details{"vaccine": "BCG"}, ""},
		{"vaccination missing", "vaccination", Details{}, "details.vaccine"},
		{"temperature ok", "temperature", Details{"celsius": 37.2}, ""},
		{"temperature missing", "temperature", Details{}, "details.celsius"},
		{"temperature high", "temperature", Details{"celsius": 45.1}, "details.celsius"},
		{"growth ok", "growth_measurement", Details{"weight_kg": 4.2}, ""},
		{"growth zero", "growth_measurement", Details{"head_cm": 0}, "details.head_cm"},
		{"growth string", "growth_measurement", Details{"height_cm": "50"}, "details.height_cm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDetails(tc.kind, tc.details)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var de *DetailsError
			if !errors.As(err, &de) || de.Field != tc.wantField {
				t.Fatalf("err=%v want DetailsError field %q", err, tc.wantField)
			}
		})
	}
}

func TestUnknownKindStillAcceptsAnyObject(t *testing.T) {
	for _, kind := range []string{"mood", "diaper", "", "Some New Kind"} {
		if err := ValidateDetails(kind, Details{"anything": []any{1, "a"}, "n": nil}); err != nil {
			t.Errorf("kind %q: %v", kind, err)
		}
	}
}

func TestDuplicateKindRegistrationPanics(t *testing.T) {
	expectPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: want panic", name)
			}
		}()
		fn()
	}
	noop := func(Details) error { return nil }
	expectPanic("duplicate built-in", func() { registerKind("feeding", noop) })
	expectPanic("empty kind", func() { registerKind("", noop) })

	registerKind("test_only_kind", noop)
	defer delete(kindValidators, "test_only_kind")
	expectPanic("duplicate new", func() { registerKind("test_only_kind", noop) })
}
