package remindercache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	interfacereminder "family-assistant/internal/interfaces/reminder"

	"github.com/redis/go-redis/v9"
)

const (
	reminderDueIndexKey    = "family-assistant:reminders:due"
	reminderIndexBatchSize = 1000
)

var errReminderIndexNeedsReconciliation = errors.New("reminder index requires reconciliation")

type redisDueReminderIndex struct {
	client            redis.UniversalClient
	dirtyVersion      atomic.Uint64
	reconciledVersion atomic.Uint64
}

func NewRedisDueReminderIndex(client redis.UniversalClient) interfacereminder.DueReminderIndex {
	if client == nil {
		return nil
	}
	return &redisDueReminderIndex{client: client}
}

func (r *redisDueReminderIndex) Upsert(ctx context.Context, reminderID string, scheduledAt time.Time) error {
	err := r.client.ZAdd(ctx, reminderDueIndexKey, redis.Z{
		Score: float64(scheduledAt.UnixMilli()), Member: reminderID,
	}).Err()
	if err != nil {
		r.markDirty()
	}
	return err
}

func (r *redisDueReminderIndex) Remove(ctx context.Context, reminderID string) error {
	err := r.client.ZRem(ctx, reminderDueIndexKey, reminderID).Err()
	if err != nil {
		r.markDirty()
	}
	return err
}

func (r *redisDueReminderIndex) HasDue(ctx context.Context, now time.Time) (bool, error) {
	if r.dirtyVersion.Load() != r.reconciledVersion.Load() {
		return false, errReminderIndexNeedsReconciliation
	}
	count, err := r.client.ZCount(ctx, reminderDueIndexKey, "-inf", strconv.FormatInt(now.UnixMilli(), 10)).Result()
	if err != nil {
		r.markDirty()
		return false, fmt.Errorf("count due reminder index: %w", err)
	}
	return count > 0, nil
}

func (r *redisDueReminderIndex) AllIDs(ctx context.Context) ([]string, error) {
	ids, err := r.client.ZRange(ctx, reminderDueIndexKey, 0, -1).Result()
	if err != nil {
		r.markDirty()
		return nil, fmt.Errorf("list reminder index: %w", err)
	}
	r.reconciledVersion.Store(r.dirtyVersion.Load())
	return ids, nil
}

func (r *redisDueReminderIndex) UpsertMany(ctx context.Context, reminders []domainreminder.Reminder) error {
	for start := 0; start < len(reminders); start += reminderIndexBatchSize {
		end := min(start+reminderIndexBatchSize, len(reminders))
		members := make([]redis.Z, 0, end-start)
		for _, reminder := range reminders[start:end] {
			members = append(members, redis.Z{Score: float64(reminder.ScheduledAt.UnixMilli()), Member: reminder.ID})
		}
		if err := r.client.ZAdd(ctx, reminderDueIndexKey, members...).Err(); err != nil {
			r.markDirty()
			return fmt.Errorf("rebuild reminder index: %w", err)
		}
	}
	return nil
}

func (r *redisDueReminderIndex) RemoveMany(ctx context.Context, reminderIDs []string) error {
	if len(reminderIDs) == 0 {
		return nil
	}
	for start := 0; start < len(reminderIDs); start += reminderIndexBatchSize {
		end := min(start+reminderIndexBatchSize, len(reminderIDs))
		members := make([]any, 0, end-start)
		for _, reminderID := range reminderIDs[start:end] {
			members = append(members, reminderID)
		}
		if err := r.client.ZRem(ctx, reminderDueIndexKey, members...).Err(); err != nil {
			r.markDirty()
			return fmt.Errorf("remove stale reminder index entries: %w", err)
		}
	}
	return nil
}

func (r *redisDueReminderIndex) markDirty() {
	r.dirtyVersion.Add(1)
}

var _ interfacereminder.DueReminderIndex = (*redisDueReminderIndex)(nil)
