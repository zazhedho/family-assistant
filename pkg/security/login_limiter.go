package security

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// LoginLimiter represents the methods required to throttle login attempts
type LoginLimiter interface {
	IsBlocked(ctx context.Context, key string) (bool, time.Duration, error)
	RegisterFailure(ctx context.Context, key string) (bool, time.Duration, error)
	Reset(ctx context.Context, key string) error
}

type redisLoginLimiter struct {
	client        *redis.Client
	limit         int
	window        time.Duration
	blockDuration time.Duration
}

// NewRedisLoginLimiter constructs a limiter backed by Redis
func NewRedisLoginLimiter(client *redis.Client, limit int, window, blockDuration time.Duration) LoginLimiter {
	if client == nil || limit <= 0 || window <= 0 || blockDuration <= 0 {
		return nil
	}

	return &redisLoginLimiter{
		client:        client,
		limit:         limit,
		window:        window,
		blockDuration: blockDuration,
	}
}

func (l *redisLoginLimiter) attemptKey(key string) string {
	return fmt.Sprintf("login_attempts:%s", key)
}

func (l *redisLoginLimiter) blockKey(key string) string {
	return fmt.Sprintf("login_block:%s", key)
}

func (l *redisLoginLimiter) IsBlocked(ctx context.Context, key string) (bool, time.Duration, error) {
	if l == nil || l.client == nil {
		return false, 0, nil
	}

	ttl, err := l.client.TTL(ctx, l.blockKey(key)).Result()
	if err != nil {
		return false, 0, err
	}

	if ttl <= 0 {
		return false, 0, nil
	}

	return true, ttl, nil
}

func (l *redisLoginLimiter) RegisterFailure(ctx context.Context, key string) (bool, time.Duration, error) {
	if l == nil || l.client == nil {
		return false, 0, nil
	}

	attemptKey := l.attemptKey(key)
	count, err := l.client.Incr(ctx, attemptKey).Result()
	if err != nil {
		return false, 0, err
	}

	if count == 1 {
		if err := l.client.Expire(ctx, attemptKey, l.window).Err(); err != nil {
			return false, 0, err
		}
	}

	if int(count) >= l.limit {
		blockKey := l.blockKey(key)
		if err := l.client.Set(ctx, blockKey, "1", l.blockDuration).Err(); err != nil {
			return false, 0, err
		}
		_ = l.client.Del(ctx, attemptKey).Err()
		return true, l.blockDuration, nil
	}

	ttl, _ := l.client.TTL(ctx, attemptKey).Result()
	return false, ttl, nil
}

func (l *redisLoginLimiter) Reset(ctx context.Context, key string) error {
	if l == nil || l.client == nil {
		return nil
	}

	if err := l.client.Del(ctx, l.attemptKey(key)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if err := l.client.Del(ctx, l.blockKey(key)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}
