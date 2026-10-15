package scheduledmessagecache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	cachehelpers "family-assistant/internal/cache"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"

	"github.com/redis/go-redis/v9"
)

const (
	scheduledMessageDueIndexKey           = "family-assistant:scheduled-messages:due"
	defaultScheduledMessageIndexBatchSize = 1000
)

var errScheduledMessageIndexNeedsReconciliation = errors.New("scheduled message index requires reconciliation")

type redisDueIndex struct {
	client    redis.UniversalClient
	batchSize int

	mu       sync.Mutex
	version  uint64
	inFlight uint64
	degraded bool
}

func NewRedisDueIndex(client redis.UniversalClient, batchSize int) interfacescheduledmessage.DueIndex {
	if client == nil {
		return nil
	}
	if batchSize <= 0 {
		batchSize = defaultScheduledMessageIndexBatchSize
	}
	return &redisDueIndex{client: client, batchSize: batchSize, version: 1, degraded: true}
}

func (r *redisDueIndex) Upsert(ctx context.Context, deliveryID string, nextAttemptAt time.Time) error {
	r.beginMutation()
	err := r.client.ZAdd(ctx, scheduledMessageDueIndexKey, redis.Z{
		Score: float64(nextAttemptAt.UnixMilli()), Member: deliveryID,
	}).Err()
	r.finishMutation(err)
	return err
}

func (r *redisDueIndex) Remove(ctx context.Context, deliveryID string) error {
	r.beginMutation()
	err := r.client.ZRem(ctx, scheduledMessageDueIndexKey, deliveryID).Err()
	r.finishMutation(err)
	return err
}

func (r *redisDueIndex) HasDue(ctx context.Context, now time.Time) (bool, error) {
	r.mu.Lock()
	degraded := r.degraded
	r.mu.Unlock()
	if degraded {
		return false, errScheduledMessageIndexNeedsReconciliation
	}
	count, err := r.client.ZCount(ctx, scheduledMessageDueIndexKey, "-inf", strconv.FormatInt(now.UnixMilli(), 10)).Result()
	if err != nil {
		r.markDirty()
		return false, fmt.Errorf("count due scheduled delivery index: %w", err)
	}
	return count > 0, nil
}

func (r *redisDueIndex) AllIDs(ctx context.Context) ([]string, error) {
	ids, err := r.client.ZRange(ctx, scheduledMessageDueIndexKey, 0, -1).Result()
	if err != nil {
		r.markDirty()
		return nil, fmt.Errorf("list scheduled delivery index: %w", err)
	}
	return ids, nil
}

func (r *redisDueIndex) UpsertMany(ctx context.Context, deliveries []domainscheduledmessage.Delivery) error {
	for _, delivery := range deliveries {
		if delivery.Status != domainscheduledmessage.DeliveryStatusPending || delivery.ID == "" || delivery.NextAttemptAt == nil {
			r.markDirty()
			return errors.New("rebuild scheduled delivery index: expected pending delivery with ID and next attempt time")
		}
	}
	if err := cachehelpers.ZAddBatched(ctx, r.client, scheduledMessageDueIndexKey, r.batchSize, deliveries, func(delivery domainscheduledmessage.Delivery) redis.Z {
		return redis.Z{Score: float64(delivery.NextAttemptAt.UnixMilli()), Member: delivery.ID}
	}); err != nil {
		r.markDirty()
		return fmt.Errorf("rebuild scheduled delivery index: %w", err)
	}
	return nil
}

func (r *redisDueIndex) RemoveMany(ctx context.Context, deliveryIDs []string) error {
	if len(deliveryIDs) == 0 {
		return nil
	}
	if err := cachehelpers.ZRemBatched(ctx, r.client, scheduledMessageDueIndexKey, r.batchSize, deliveryIDs); err != nil {
		r.markDirty()
		return fmt.Errorf("remove stale scheduled delivery index entries: %w", err)
	}
	return nil
}

func (r *redisDueIndex) ReconcileVersion() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

func (r *redisDueIndex) MarkReconciled(version uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.version == version && r.inFlight == 0 {
		r.degraded = false
	} else {
		r.degraded = true
	}
}

func (r *redisDueIndex) beginMutation() {
	r.mu.Lock()
	r.version++
	r.inFlight++
	r.mu.Unlock()
}

func (r *redisDueIndex) BeginLifecycleMutation() {
	r.beginMutation()
}

func (r *redisDueIndex) finishMutation(err error) {
	r.mu.Lock()
	r.version++
	r.inFlight--
	if err != nil {
		r.degraded = true
	}
	r.mu.Unlock()
}

func (r *redisDueIndex) FinishLifecycleMutation(err error) {
	r.finishMutation(err)
}

func (r *redisDueIndex) markDirty() {
	r.mu.Lock()
	r.version++
	r.degraded = true
	r.mu.Unlock()
}

var _ interfacescheduledmessage.DueIndex = (*redisDueIndex)(nil)
