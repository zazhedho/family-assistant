package integration

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	reminderRepo "family-assistant/internal/repositories/reminder"
	"family-assistant/utils"

	"gorm.io/gorm"
)

type replaceFixture struct {
	spaceID  string
	memberID string
}

func TestReminderReplaceKeyAgainstPostgres(t *testing.T) {
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

	repo := reminderRepo.NewRepository(db)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second).Add(24 * time.Hour)
	now := time.Now().UTC().Truncate(time.Millisecond)

	newReminder := func(f replaceFixture, key *string, at time.Time) *domainreminder.Reminder {
		return &domainreminder.Reminder{
			SpaceID:           f.spaceID,
			CreatedByMemberID: f.memberID,
			Title:             "care",
			ScheduledAt:       at,
			Status:            domainreminder.StatusPending,
			ReplaceKey:        key,
		}
	}
	pendingIDs := func(t *testing.T, f replaceFixture, key string) []string {
		t.Helper()
		var ids []string
		if err := db.Raw(`SELECT id FROM reminders WHERE space_id = ? AND replace_key = ? AND status = 'PENDING' AND deleted_at IS NULL`, f.spaceID, key).Scan(&ids).Error; err != nil {
			t.Fatal(err)
		}
		return ids
	}
	strPtr := func(s string) *string { return &s }

	t.Run("replaced", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "replaced")
		key := "care-replaced"
		first, err := repo.CreateReplacing(ctx, newReminder(f, &key, base), now)
		if err != nil || first.Outcome != domainreminder.ReplaceOutcomeCreated {
			t.Fatalf("first = %+v, %v; want created", first, err)
		}
		second, err := repo.CreateReplacing(ctx, newReminder(f, &key, base.Add(time.Hour)), now)
		if err != nil || second.Outcome != domainreminder.ReplaceOutcomeReplaced {
			t.Fatalf("second = %+v, %v; want replaced", second, err)
		}
		if len(second.Replaced) != 1 || second.Replaced[0].ID != first.Reminder.ID {
			t.Fatalf("replaced = %+v, want only the first reminder", second.Replaced)
		}
		ids := pendingIDs(t, f, key)
		if len(ids) != 1 || ids[0] != second.Reminder.ID {
			t.Fatalf("pending ids = %v, want only %s", ids, second.Reminder.ID)
		}
		var status string
		var deleted bool
		if err := db.Raw(`SELECT status, deleted_at IS NOT NULL FROM reminders WHERE id = ?`, first.Reminder.ID).Row().Scan(&status, &deleted); err != nil {
			t.Fatal(err)
		}
		if status != "CANCELLED" || !deleted {
			t.Fatalf("old row status=%s deleted=%v, want CANCELLED and soft-deleted", status, deleted)
		}
	})

	t.Run("kept_existing", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "kept")
		key := "care-kept"
		later, err := repo.CreateReplacing(ctx, newReminder(f, &key, base.Add(2*time.Hour)), now)
		if err != nil || later.Outcome != domainreminder.ReplaceOutcomeCreated {
			t.Fatalf("later = %+v, %v; want created", later, err)
		}
		earlier, err := repo.CreateReplacing(ctx, newReminder(f, &key, base), now)
		if err != nil || earlier.Outcome != domainreminder.ReplaceOutcomeKeptExisting {
			t.Fatalf("earlier = %+v, %v; want kept_existing", earlier, err)
		}
		if earlier.Reminder == nil || earlier.Reminder.ID != later.Reminder.ID {
			t.Fatalf("kept reminder = %+v, want %s", earlier.Reminder, later.Reminder.ID)
		}
		var total int64
		if err := db.Raw(`SELECT COUNT(*) FROM reminders WHERE space_id = ? AND replace_key = ?`, f.spaceID, key).Scan(&total).Error; err != nil {
			t.Fatal(err)
		}
		if total != 1 {
			t.Fatalf("rows = %d, want 1 (nothing inserted)", total)
		}
		if ids := pendingIDs(t, f, key); len(ids) != 1 || ids[0] != later.Reminder.ID {
			t.Fatalf("pending ids = %v, want only %s", ids, later.Reminder.ID)
		}
	})

	t.Run("isolation", func(t *testing.T) {
		a := seedReplaceFixture(t, db, "iso-a")
		b := seedReplaceFixture(t, db, "iso-b")
		key := "care-iso"
		inA, err := repo.CreateReplacing(ctx, newReminder(a, &key, base), now)
		if err != nil {
			t.Fatal(err)
		}
		inB, err := repo.CreateReplacing(ctx, newReminder(b, &key, base), now)
		if err != nil || inB.Outcome != domainreminder.ReplaceOutcomeCreated {
			t.Fatalf("other space = %+v, %v; want created", inB, err)
		}
		nullOne, err := repo.CreateReplacing(ctx, newReminder(a, nil, base), now)
		if err != nil || nullOne.Outcome != domainreminder.ReplaceOutcomeNone {
			t.Fatalf("null key one = %+v, %v; want none", nullOne, err)
		}
		nullTwo, err := repo.CreateReplacing(ctx, newReminder(a, nil, base), now)
		if err != nil || nullTwo.Outcome != domainreminder.ReplaceOutcomeNone {
			t.Fatalf("null key two = %+v, %v; want none", nullTwo, err)
		}
		replacement, err := repo.CreateReplacing(ctx, newReminder(a, &key, base.Add(time.Hour)), now)
		if err != nil || replacement.Outcome != domainreminder.ReplaceOutcomeReplaced {
			t.Fatalf("replacement = %+v, %v; want replaced", replacement, err)
		}
		if len(replacement.Replaced) != 1 || replacement.Replaced[0].ID != inA.Reminder.ID {
			t.Fatalf("replaced = %+v, want only space A's first reminder", replacement.Replaced)
		}
		if ids := pendingIDs(t, b, key); len(ids) != 1 || ids[0] != inB.Reminder.ID {
			t.Fatalf("space B pending = %v, want untouched %s", ids, inB.Reminder.ID)
		}
		var nullPending int64
		if err := db.Raw(`SELECT COUNT(*) FROM reminders WHERE space_id = ? AND replace_key IS NULL AND status = 'PENDING' AND deleted_at IS NULL`, a.spaceID).Scan(&nullPending).Error; err != nil {
			t.Fatal(err)
		}
		if nullPending != 2 {
			t.Fatalf("null-key pending rows = %d, want 2 untouched", nullPending)
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		f := seedReplaceFixture(t, db, "concurrent")
		const workers = 4
		key := "care-concurrent"
		latest := base.Add(workers * time.Hour)
		start := make(chan struct{})
		errs := make([]error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				r := newReminder(f, strPtr(key), base.Add(time.Duration(i+1)*time.Hour))
				<-start
				_, errs[i] = repo.CreateReplacing(ctx, r, now)
			}(i)
		}
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil && !errors.Is(err, domainreminder.ErrReplaceKeyConflict) {
				t.Fatalf("worker %d: unexpected error %v", i, err)
			}
		}
		var rows []domainreminder.Reminder
		if err := db.Where("space_id = ? AND replace_key = ? AND status = ?", f.spaceID, key, domainreminder.StatusPending).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("pending non-deleted rows = %d, want exactly 1 (errs=%v)", len(rows), errs)
		}
		// A worker that exhausts its single retry returns ErrReplaceKeyConflict
		// and writes nothing, so the latest time only survives when its worker
		// succeeded. In every case the survivor must not be earlier than any
		// successful worker's time.
		var maxSucceeded time.Time
		for i, err := range errs {
			at := base.Add(time.Duration(i+1) * time.Hour)
			if err == nil && at.After(maxSucceeded) {
				maxSucceeded = at
			}
		}
		if rows[0].ScheduledAt.Before(maxSucceeded) {
			t.Fatalf("surviving row scheduled_at = %s, earlier than successful worker at %s (errs=%v)", rows[0].ScheduledAt, maxSucceeded, errs)
		}
		if errs[workers-1] == nil && !rows[0].ScheduledAt.Equal(latest) {
			t.Fatalf("latest worker succeeded but survivor = %s, want %s", rows[0].ScheduledAt, latest)
		}
	})
}

// seedReplaceFixture creates the minimum prerequisite rows for reminders: a
// user, a personal space, and the owner's space member.
func seedReplaceFixture(t *testing.T, db *gorm.DB, label string) replaceFixture {
	t.Helper()
	userID, spaceID, memberID := utils.CreateUUID(), utils.CreateUUID(), utils.CreateUUID()
	var roleID string
	if err := db.Raw(`SELECT id FROM roles ORDER BY created_at LIMIT 1`).Scan(&roleID).Error; err != nil || roleID == "" {
		t.Fatalf("seed role lookup: %q, %v", roleID, err)
	}
	steps := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, name, email, password) VALUES (?, ?, ?, '')`, []any{userID, "Replace " + label, label + "@replace.example.test"}},
		{`INSERT INTO spaces (id, name, type, category, created_by_user_id) VALUES (?, ?, 'PERSONAL', 'personal', ?)`, []any{spaceID, "Space " + label, userID}},
		{`INSERT INTO space_members (id, space_id, user_id, role_id) VALUES (?, ?, ?, ?)`, []any{memberID, spaceID, userID, roleID}},
	}
	for _, step := range steps {
		if err := db.Exec(step.sql, step.args...).Error; err != nil {
			t.Fatalf("seed fixture %s: %v", label, err)
		}
	}
	return replaceFixture{spaceID: spaceID, memberID: memberID}
}
