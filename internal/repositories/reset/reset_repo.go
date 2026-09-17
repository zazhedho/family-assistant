package repositoryreset

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type PasswordResetRepository struct {
	Redis *redis.Client
}

func NewPasswordResetRepository(redisClient *redis.Client) *PasswordResetRepository {
	return &PasswordResetRepository{Redis: redisClient}
}

const (
	resetTokenKeyPrefix    = "reset:token:"
	resetCooldownKeyPrefix = "reset:cooldown:"
	resetRateKeyPrefix     = "reset:rate:"
)

func (r *PasswordResetRepository) SetToken(ctx context.Context, hash, email string, ttl time.Duration) error {
	return r.Redis.Set(ctx, fmt.Sprintf("%s%s", resetTokenKeyPrefix, hash), email, ttl).Err()
}

func (r *PasswordResetRepository) GetEmailByToken(ctx context.Context, hash string) (string, error) {
	return r.Redis.Get(ctx, fmt.Sprintf("%s%s", resetTokenKeyPrefix, hash)).Result()
}

func (r *PasswordResetRepository) DeleteToken(ctx context.Context, hash string) error {
	return r.Redis.Del(ctx, fmt.Sprintf("%s%s", resetTokenKeyPrefix, hash)).Err()
}

func (r *PasswordResetRepository) SetCooldown(ctx context.Context, email string, ttl time.Duration) error {
	return r.Redis.Set(ctx, fmt.Sprintf("%s%s", resetCooldownKeyPrefix, email), "1", ttl).Err()
}

func (r *PasswordResetRepository) GetCooldownTTL(ctx context.Context, email string) (time.Duration, error) {
	return r.Redis.TTL(ctx, fmt.Sprintf("%s%s", resetCooldownKeyPrefix, email)).Result()
}

func (r *PasswordResetRepository) ClearCooldown(ctx context.Context, email string) error {
	return r.Redis.Del(ctx, fmt.Sprintf("%s%s", resetCooldownKeyPrefix, email)).Err()
}

func (r *PasswordResetRepository) IncrementSendCount(ctx context.Context, email string, ttl time.Duration) (int, time.Duration, error) {
	key := fmt.Sprintf("%s%s", resetRateKeyPrefix, email)
	count, err := r.Redis.Incr(ctx, key).Result()
	if err != nil {
		return 0, 0, err
	}
	if count == 1 && ttl > 0 {
		_ = r.Redis.Expire(ctx, key, ttl).Err()
	}
	retryAfter, _ := r.Redis.TTL(ctx, key).Result()
	return int(count), retryAfter, nil
}

func (r *PasswordResetRepository) ClearSendCount(ctx context.Context, email string) error {
	return r.Redis.Del(ctx, fmt.Sprintf("%s%s", resetRateKeyPrefix, email)).Err()
}
