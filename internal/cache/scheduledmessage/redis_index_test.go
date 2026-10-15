package scheduledmessagecache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

func newHealthyDueIndex(client redis.UniversalClient) interfacescheduledmessage.DueIndex {
	index := NewRedisDueIndex(client, 1000)
	index.MarkReconciled(index.ReconcileVersion())
	return index
}

func TestRedisDueIndexChecksIdleTickInRedis(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	now := time.UnixMilli(1_800_000_000_000)

	mock.ExpectZCount(scheduledMessageDueIndexKey, "-inf", "1800000000000").SetVal(0)
	due, err := index.HasDue(context.Background(), now)
	if err != nil {
		t.Fatalf("check due deliveries: %v", err)
	}
	if due {
		t.Fatal("HasDue = true, want false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexRequiresStartupReconciliation(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueIndex(client, 1000)

	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want startup reconciliation error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexRetryMovesDeliveryScore(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueIndex(client, 1000)
	first := time.UnixMilli(1_800_000_000_100)
	retry := time.UnixMilli(1_800_000_000_900)

	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: 1_800_000_000_100, Member: "delivery-uuid"}).SetVal(1)
	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: 1_800_000_000_900, Member: "delivery-uuid"}).SetVal(0)
	if err := index.Upsert(context.Background(), "delivery-uuid", first); err != nil {
		t.Fatalf("upsert delivery: %v", err)
	}
	if err := index.Upsert(context.Background(), "delivery-uuid", retry); err != nil {
		t.Fatalf("move retry delivery: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexCancellationRemovesDelivery(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueIndex(client, 1000)

	mock.ExpectZRem(scheduledMessageDueIndexKey, "delivery-uuid").SetVal(1)
	if err := index.Remove(context.Background(), "delivery-uuid"); err != nil {
		t.Fatalf("remove cancelled delivery: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexFailedWriteNeedsReconciliation(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	writeErr := errors.New("redis unavailable")

	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: 1_800_000_000_100, Member: "delivery-uuid"}).SetErr(writeErr)
	if err := index.Upsert(context.Background(), "delivery-uuid", time.UnixMilli(1_800_000_000_100)); !errors.Is(err, writeErr) {
		t.Fatalf("upsert error = %v, want %v", err, writeErr)
	}

	version := index.ReconcileVersion()
	mock.ExpectZRange(scheduledMessageDueIndexKey, 0, -1).SetVal([]string{})
	if ids, err := index.AllIDs(context.Background()); err != nil || len(ids) != 0 {
		t.Fatalf("AllIDs = %v, %v; want empty result", ids, err)
	}
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error without Redis count", err)
	}

	index.MarkReconciled(version)
	mock.ExpectZCount(scheduledMessageDueIndexKey, "-inf", "1800000000000").SetVal(0)
	if due, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err != nil || due {
		t.Fatalf("HasDue after acknowledged reconciliation = %v, %v; want false, nil", due, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexConcurrentWriteFailureKeepsNewerVersionDirty(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	capturedVersion := index.ReconcileVersion()
	writeErr := errors.New("redis unavailable")

	mock.ExpectZRange(scheduledMessageDueIndexKey, 0, -1).SetVal([]string{})
	if _, err := index.AllIDs(context.Background()); err != nil {
		t.Fatalf("list indexed IDs: %v", err)
	}
	mock.ExpectZRem(scheduledMessageDueIndexKey, "delivery-uuid").SetErr(writeErr)
	if err := index.Remove(context.Background(), "delivery-uuid"); !errors.Is(err, writeErr) {
		t.Fatalf("remove error = %v, want %v", err, writeErr)
	}
	index.MarkReconciled(capturedVersion)
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error without Redis count", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexSuccessfulWriteDuringRepairInvalidatesAcknowledgement(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	capturedVersion := index.ReconcileVersion()

	mock.ExpectZRange(scheduledMessageDueIndexKey, 0, -1).SetVal([]string{})
	if _, err := index.AllIDs(context.Background()); err != nil {
		t.Fatalf("list indexed IDs: %v", err)
	}
	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: 1_800_000_000_100, Member: "delivery-uuid"}).SetVal(1)
	if err := index.Upsert(context.Background(), "delivery-uuid", time.UnixMilli(1_800_000_000_100)); err != nil {
		t.Fatalf("upsert delivery during repair: %v", err)
	}
	index.MarkReconciled(capturedVersion)
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error without Redis count", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexSuccessfulRemovalDuringRepairInvalidatesAcknowledgement(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	capturedVersion := index.ReconcileVersion()

	mock.ExpectZRange(scheduledMessageDueIndexKey, 0, -1).SetVal([]string{"delivery-uuid"})
	if _, err := index.AllIDs(context.Background()); err != nil {
		t.Fatalf("list indexed IDs: %v", err)
	}
	mock.ExpectZRem(scheduledMessageDueIndexKey, "delivery-uuid").SetVal(1)
	if err := index.Remove(context.Background(), "delivery-uuid"); err != nil {
		t.Fatalf("remove delivery during repair: %v", err)
	}
	index.MarkReconciled(capturedVersion)
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error without Redis count", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexLifecycleMutationKeepsPrecommitRepairDirty(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	capturedVersion := index.ReconcileVersion()

	mock.ExpectZRange(scheduledMessageDueIndexKey, 0, -1).SetVal([]string{})
	if _, err := index.AllIDs(context.Background()); err != nil {
		t.Fatalf("list indexed IDs: %v", err)
	}
	index.BeginLifecycleMutation()
	index.MarkReconciled(capturedVersion)
	index.FinishLifecycleMutation(nil)
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want stale repair acknowledgement to stay dirty", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexHealthySingleWriteKeepsHasDueAvailable(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	now := time.UnixMilli(1_800_000_000_000)

	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: 1_800_000_000_100, Member: "delivery-uuid"}).SetVal(1)
	if err := index.Upsert(context.Background(), "delivery-uuid", time.UnixMilli(1_800_000_000_100)); err != nil {
		t.Fatalf("upsert delivery: %v", err)
	}
	mock.ExpectZCount(scheduledMessageDueIndexKey, "-inf", "1800000000000").SetVal(0)
	if _, err := index.HasDue(context.Background(), now); err != nil {
		t.Fatalf("HasDue after successful write = %v, want nil", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexRebuildUsesPendingDeliveryIDAndRetryTime(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueIndex(client, 1000)
	nextAttemptAt := time.UnixMilli(1_800_000_000_900)

	mock.ExpectZAdd(scheduledMessageDueIndexKey,
		redis.Z{Score: 1_800_000_000_900, Member: "delivery-uuid"},
	).SetVal(1)
	if err := index.UpsertMany(context.Background(), []domainscheduledmessage.Delivery{{
		ID: "delivery-uuid", ScheduledMessageID: "schedule-uuid", Status: domainscheduledmessage.DeliveryStatusPending, NextAttemptAt: &nextAttemptAt,
	}}); err != nil {
		t.Fatalf("rebuild pending deliveries: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexCountFailureMarksIndexDegraded(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	readErr := errors.New("redis unavailable")

	mock.ExpectZCount(scheduledMessageDueIndexKey, "-inf", "1800000000000").SetErr(readErr)
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); !errors.Is(err, readErr) {
		t.Fatalf("HasDue error = %v, want %v", err, readErr)
	}
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("second HasDue error = %v, want reconciliation error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexListingFailureMarksIndexDegraded(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	readErr := errors.New("redis unavailable")

	mock.ExpectZRange(scheduledMessageDueIndexKey, 0, -1).SetErr(readErr)
	if _, err := index.AllIDs(context.Background()); !errors.Is(err, readErr) {
		t.Fatalf("AllIDs error = %v, want %v", err, readErr)
	}
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexBatchWriteFailureMarksIndexDegraded(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	nextAttemptAt := time.UnixMilli(1_800_000_000_900)
	writeErr := errors.New("redis unavailable")

	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: 1_800_000_000_900, Member: "delivery-uuid"}).SetErr(writeErr)
	if err := index.UpsertMany(context.Background(), []domainscheduledmessage.Delivery{{
		ID: "delivery-uuid", Status: domainscheduledmessage.DeliveryStatusPending, NextAttemptAt: &nextAttemptAt,
	}}); !errors.Is(err, writeErr) {
		t.Fatalf("UpsertMany error = %v, want %v", err, writeErr)
	}
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexRejectsPendingDeliveryWithoutIDOrRetryTime(t *testing.T) {
	for _, delivery := range []domainscheduledmessage.Delivery{
		{ID: "delivery-uuid", Status: domainscheduledmessage.DeliveryStatusPending},
		{Status: domainscheduledmessage.DeliveryStatusPending, NextAttemptAt: timePtr(time.UnixMilli(1_800_000_000_900))},
	} {
		client, mock := redismock.NewClientMock()
		index := newHealthyDueIndex(client)
		if err := index.UpsertMany(context.Background(), []domainscheduledmessage.Delivery{delivery}); err == nil {
			t.Fatal("UpsertMany error = nil, want validation error")
		}
		if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
			t.Fatalf("HasDue error = %v, want reconciliation error", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	}
}

func TestRedisDueIndexRemovesReconciledTerminalIDsInBatches(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := NewRedisDueIndex(client, 1)

	mock.ExpectZRem(scheduledMessageDueIndexKey, "delivery-1").SetVal(1)
	mock.ExpectZRem(scheduledMessageDueIndexKey, "delivery-2").SetVal(1)
	if err := index.RemoveMany(context.Background(), []string{"delivery-1", "delivery-2"}); err != nil {
		t.Fatalf("remove terminal deliveries: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisDueIndexBatchRemovalFailureMarksIndexDegraded(t *testing.T) {
	client, mock := redismock.NewClientMock()
	index := newHealthyDueIndex(client)
	writeErr := errors.New("redis unavailable")

	mock.ExpectZRem(scheduledMessageDueIndexKey, "delivery-uuid").SetErr(writeErr)
	if err := index.RemoveMany(context.Background(), []string{"delivery-uuid"}); !errors.Is(err, writeErr) {
		t.Fatalf("RemoveMany error = %v, want %v", err, writeErr)
	}
	if _, err := index.HasDue(context.Background(), time.UnixMilli(1_800_000_000_000)); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func timePtr(value time.Time) *time.Time { return &value }

type delayedZAddClient struct {
	redis.UniversalClient
	started     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	calls       atomic.Int32
}

func (c *delayedZAddClient) ZAdd(ctx context.Context, key string, members ...redis.Z) *redis.IntCmd {
	if c.calls.Add(1) == 1 {
		close(c.started)
		<-c.release
	}
	return c.UniversalClient.ZAdd(ctx, key, members...)
}

func (c *delayedZAddClient) unblock() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func TestRedisDueIndexDelayedOlderWriteCannotBeAcknowledgedHealthy(t *testing.T) {
	baseClient, mock := redismock.NewClientMock()
	client := &delayedZAddClient{
		UniversalClient: baseClient,
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	t.Cleanup(client.unblock)
	index := newHealthyDueIndex(client)
	now := time.UnixMilli(1_800_000_000_000)
	olderAttempt := now.Add(time.Second)
	newerAttempt := now.Add(-time.Second)
	const deliveryID = "delivery-uuid"

	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: float64(newerAttempt.UnixMilli()), Member: deliveryID}).SetVal(1)
	mock.ExpectZRange(scheduledMessageDueIndexKey, 0, -1).SetVal([]string{deliveryID})
	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: float64(newerAttempt.UnixMilli()), Member: deliveryID}).SetVal(0)
	mock.ExpectZAdd(scheduledMessageDueIndexKey, redis.Z{Score: float64(olderAttempt.UnixMilli()), Member: deliveryID}).SetVal(0)

	olderWrite := make(chan error, 1)
	go func() {
		olderWrite <- index.Upsert(context.Background(), deliveryID, olderAttempt)
	}()
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		t.Fatal("older Redis write did not reach the blocking boundary")
	}

	if err := index.Upsert(context.Background(), deliveryID, newerAttempt); err != nil {
		t.Fatalf("upsert newer delivery: %v", err)
	}
	capturedVersion := index.ReconcileVersion()
	if _, err := index.AllIDs(context.Background()); err != nil {
		t.Fatalf("list indexed IDs: %v", err)
	}
	if err := index.UpsertMany(context.Background(), []domainscheduledmessage.Delivery{{
		ID: deliveryID, Status: domainscheduledmessage.DeliveryStatusPending, NextAttemptAt: &newerAttempt,
	}}); err != nil {
		t.Fatalf("rebuild pending delivery: %v", err)
	}
	if err := index.RemoveMany(context.Background(), nil); err != nil {
		t.Fatalf("remove terminal deliveries: %v", err)
	}
	index.MarkReconciled(capturedVersion)

	client.unblock()
	select {
	case err := <-olderWrite:
		if err != nil {
			t.Fatalf("finish older Redis write: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("older Redis write did not finish after release")
	}

	if _, err := index.HasDue(context.Background(), now); err == nil || !strings.Contains(err.Error(), "requires reconciliation") {
		t.Fatalf("HasDue error = %v, want reconciliation error after stale write", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
