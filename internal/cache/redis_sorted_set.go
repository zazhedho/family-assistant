package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func ZAddBatched[T any](ctx context.Context, client redis.UniversalClient, key string, batchSize int, values []T, member func(T) redis.Z) error {
	if batchSize <= 0 {
		return fmt.Errorf("redis sorted-set batch size must be positive: %d", batchSize)
	}
	for start := 0; start < len(values); start += batchSize {
		end := min(start+batchSize, len(values))
		members := make([]redis.Z, 0, end-start)
		for _, value := range values[start:end] {
			members = append(members, member(value))
		}
		if err := client.ZAdd(ctx, key, members...).Err(); err != nil {
			return err
		}
	}
	return nil
}

func ZRemBatched(ctx context.Context, client redis.UniversalClient, key string, batchSize int, members []string) error {
	if batchSize <= 0 {
		return fmt.Errorf("redis sorted-set batch size must be positive: %d", batchSize)
	}
	for start := 0; start < len(members); start += batchSize {
		end := min(start+batchSize, len(members))
		batch := make([]any, 0, end-start)
		for _, member := range members[start:end] {
			batch = append(batch, member)
		}
		if err := client.ZRem(ctx, key, batch...).Err(); err != nil {
			return err
		}
	}
	return nil
}
