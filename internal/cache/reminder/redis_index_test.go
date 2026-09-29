package remindercache

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

func TestRedisDueReminderIndexUsesSortedSetForScheduling(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueReminderIndex(client, 1000)
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	scheduledAt := now.Add(time.Hour)

	mock.ExpectZAdd(reminderDueIndexKey, redis.Z{Score: float64(scheduledAt.UnixMilli()), Member: "reminder-1"}).SetVal(1)
	if err := index.Upsert(context.Background(), "reminder-1", scheduledAt); err != nil {
		t.Fatalf("upsert reminder: %v", err)
	}

	mock.ExpectZCount(reminderDueIndexKey, "-inf", strconv.FormatInt(now.UnixMilli(), 10)).SetVal(1)
	due, err := index.HasDue(context.Background(), now)
	if err != nil {
		t.Fatalf("check due reminders: %v", err)
	}
	if !due {
		t.Fatal("HasDue = false, want true")
	}

	mock.ExpectZRem(reminderDueIndexKey, "reminder-1").SetVal(1)
	if err := index.Remove(context.Background(), "reminder-1"); err != nil {
		t.Fatalf("remove reminder: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueReminderIndexListsAndReconcilesEntries(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueReminderIndex(client, 1000)
	firstAt := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Hour)

	mock.ExpectZRange(reminderDueIndexKey, 0, -1).SetVal([]string{"reminder-1", "stale-reminder"})
	ids, err := index.AllIDs(context.Background())
	if err != nil {
		t.Fatalf("list indexed reminder IDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != "reminder-1" || ids[1] != "stale-reminder" {
		t.Fatalf("indexed IDs = %v", ids)
	}

	mock.ExpectZAdd(reminderDueIndexKey,
		redis.Z{Score: float64(firstAt.UnixMilli()), Member: "reminder-1"},
		redis.Z{Score: float64(secondAt.UnixMilli()), Member: "reminder-2"},
	).SetVal(2)
	if err := index.UpsertMany(context.Background(), []domainreminder.Reminder{
		{ID: "reminder-1", ScheduledAt: firstAt},
		{ID: "reminder-2", ScheduledAt: secondAt},
	}); err != nil {
		t.Fatalf("bulk upsert reminders: %v", err)
	}

	mock.ExpectZRem(reminderDueIndexKey, "stale-reminder").SetVal(1)
	if err := index.RemoveMany(context.Background(), []string{"stale-reminder"}); err != nil {
		t.Fatalf("bulk remove reminders: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueReminderIndexUsesConfiguredBatchSize(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueReminderIndex(client, 1)
	firstAt := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Hour)

	mock.ExpectZAdd(reminderDueIndexKey, redis.Z{Score: float64(firstAt.UnixMilli()), Member: "reminder-1"}).SetVal(1)
	mock.ExpectZAdd(reminderDueIndexKey, redis.Z{Score: float64(secondAt.UnixMilli()), Member: "reminder-2"}).SetVal(1)
	mock.ExpectZRem(reminderDueIndexKey, "reminder-1").SetVal(1)
	mock.ExpectZRem(reminderDueIndexKey, "reminder-2").SetVal(1)
	if err := index.UpsertMany(context.Background(), []domainreminder.Reminder{
		{ID: "reminder-1", ScheduledAt: firstAt},
		{ID: "reminder-2", ScheduledAt: secondAt},
	}); err != nil {
		t.Fatalf("bulk upsert reminders: %v", err)
	}
	if err := index.RemoveMany(context.Background(), []string{"reminder-1", "reminder-2"}); err != nil {
		t.Fatalf("bulk remove reminders: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueReminderIndexRequiresReconciliationAfterWriteFailure(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueReminderIndex(client, 1000)
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	scheduledAt := now.Add(time.Hour)
	writeErr := errors.New("redis unavailable")

	mock.ExpectZAdd(reminderDueIndexKey, redis.Z{Score: float64(scheduledAt.UnixMilli()), Member: "reminder-1"}).SetErr(writeErr)
	if err := index.Upsert(context.Background(), "reminder-1", scheduledAt); !errors.Is(err, writeErr) {
		t.Fatalf("upsert error = %v, want %v", err, writeErr)
	}
	if _, err := index.HasDue(context.Background(), now); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want index reconciliation error", err)
	}

	mock.ExpectZRange(reminderDueIndexKey, 0, -1).SetVal([]string{})
	if _, err := index.AllIDs(context.Background()); err != nil {
		t.Fatalf("list index for reconciliation: %v", err)
	}
	mock.ExpectZAdd(reminderDueIndexKey, redis.Z{Score: float64(scheduledAt.UnixMilli()), Member: "reminder-1"}).SetVal(1)
	if err := index.UpsertMany(context.Background(), []domainreminder.Reminder{{ID: "reminder-1", ScheduledAt: scheduledAt}}); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}
	if err := index.RemoveMany(context.Background(), nil); err != nil {
		t.Fatalf("finish reconciliation: %v", err)
	}
	mock.ExpectZCount(reminderDueIndexKey, "-inf", strconv.FormatInt(now.UnixMilli(), 10)).SetVal(0)
	if _, err := index.HasDue(context.Background(), now); err != nil {
		t.Fatalf("check healthy index after reconciliation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
