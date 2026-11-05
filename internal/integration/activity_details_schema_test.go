package integration

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestActivityDetailsConstraintsAgainstPostgres(t *testing.T) {
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

	f := seedReplaceFixture(t, db, "details")
	insert := func(kind, details string) error {
		return db.Exec(
			`INSERT INTO space_activities (space_id, created_by_member_id, kind, note, occurred_at, details)
			 VALUES (?, ?, ?, '', ?, ?::jsonb)`,
			f.spaceID, f.memberID, kind, time.Now().UTC(), details,
		).Error
	}
	big := `{"x":"` + strings.Repeat("a", 4100) + `"}`

	cases := []struct {
		name    string
		kind    string
		details string
		wantErr bool
	}{
		{"accepts empty details", "diaper", `{}`, false},
		{"accepts feeding volume", "feeding", `{"volume_ml":60}`, false},
		{"accepts legacy feeding empty", "feeding", `{}`, false},
		{"accepts full expired feeding", "feeding", `{"volume_ml":90,"leftover_ml":70,"outcome":"expired","expired_ml":40}`, false},
		{"accepts non-feeding arbitrary object", "diaper", `{"volume_ml":-5,"outcome":"burped","foo":[1]}`, false},
		{"accepts leftover_reported_at string", "feeding", `{"volume_ml":60,"leftover_ml":20,"leftover_reported_at":"2026-11-05T13:58:47+07:00"}`, false},
		{"rejects leftover_reported_at number", "feeding", `{"volume_ml":60,"leftover_reported_at":123}`, true},
		{"rejects leftover_reported_at null", "feeding", `{"volume_ml":60,"leftover_reported_at":null}`, true},
		{"rejects array details", "diaper", `[1]`, true},
		{"rejects oversized details", "diaper", big, true},
		{"rejects leftover over volume", "feeding", `{"volume_ml":60,"leftover_ml":70}`, true},
		{"rejects expired without expired_ml", "feeding", `{"volume_ml":60,"outcome":"expired"}`, true},
		{"rejects unknown outcome", "feeding", `{"volume_ml":60,"outcome":"burped"}`, true},
		{"rejects negative volume", "feeding", `{"volume_ml":-1}`, true},
		{"rejects non-numeric volume", "feeding", `{"volume_ml":"lots"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := insert(tc.kind, tc.details)
			if tc.wantErr && err == nil {
				t.Fatalf("expected rejection for %s %s", tc.kind, tc.details)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected accept, got %v", err)
			}
		})
	}
}
