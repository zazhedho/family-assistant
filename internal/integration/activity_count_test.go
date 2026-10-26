package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	activityRepo "family-assistant/internal/repositories/activity"
)

func TestActivityCountByKindAgainstPostgres(t *testing.T) {
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
	f := seedReplaceFixture(t, db, "count")
	other := seedReplaceFixture(t, db, "count-other")
	midnight := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	add := func(fixture replaceFixture, kind string, at time.Time) *domainactivity.Activity {
		activity := &domainactivity.Activity{SpaceID: fixture.spaceID, CreatedByMemberID: fixture.memberID, Kind: kind, OccurredAt: at}
		if err := repo.Create(ctx, activity); err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		return activity
	}
	add(f, "diaper", midnight.Add(time.Hour))
	add(f, "diaper", midnight.Add(5*time.Hour))
	add(f, "bowel_movement", midnight.Add(5*time.Hour))
	add(f, "diaper", midnight.Add(-time.Minute))    // before midnight
	add(f, "feeding", midnight.Add(2*time.Hour))    // other kind
	add(other, "diaper", midnight.Add(2*time.Hour)) // other space
	deleted := add(f, "diaper", midnight.Add(3*time.Hour))
	if err := repo.SoftDelete(ctx, f.spaceID, deleted.ID, midnight.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}

	counts, err := repo.CountByKind(ctx, f.spaceID, []string{"diaper", "bowel_movement"}, midnight, midnight.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("count by kind: %v", err)
	}
	if counts["diaper"] != 2 || counts["bowel_movement"] != 1 || len(counts) != 2 {
		t.Fatalf("counts = %v, want diaper 2 and bowel_movement 1", counts)
	}
}
