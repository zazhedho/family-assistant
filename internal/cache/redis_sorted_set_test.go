package cache

import (
	"context"
	"testing"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

func TestZAddBatchedUsesConfiguredBatchSize(t *testing.T) {
	client, mock := redismock.NewClientMock()
	members := []redis.Z{
		{Score: 1, Member: "one"},
		{Score: 2, Member: "two"},
		{Score: 3, Member: "three"},
	}

	mock.ExpectZAdd("key", members[:2]...).SetVal(2)
	mock.ExpectZAdd("key", members[2]).SetVal(1)
	if err := ZAddBatched(context.Background(), client, "key", 2, members, func(member redis.Z) redis.Z {
		return member
	}); err != nil {
		t.Fatalf("batched ZADD: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestZRemBatchedUsesConfiguredBatchSize(t *testing.T) {
	client, mock := redismock.NewClientMock()
	members := []string{"one", "two", "three"}

	mock.ExpectZRem("key", members[0], members[1]).SetVal(2)
	mock.ExpectZRem("key", members[2]).SetVal(1)
	if err := ZRemBatched(context.Background(), client, "key", 2, members); err != nil {
		t.Fatalf("batched ZREM: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestBatchedSortedSetOperationsRejectNonpositiveBatchSize(t *testing.T) {
	client, _ := redismock.NewClientMock()
	for _, batchSize := range []int{0, -1} {
		if err := ZAddBatched(context.Background(), client, "key", batchSize, []string{"one"}, func(member string) redis.Z {
			return redis.Z{Member: member}
		}); err == nil {
			t.Fatalf("ZAddBatched batch size %d error = nil", batchSize)
		}
		if err := ZRemBatched(context.Background(), client, "key", batchSize, []string{"one"}); err == nil {
			t.Fatalf("ZRemBatched batch size %d error = nil", batchSize)
		}
	}
}
