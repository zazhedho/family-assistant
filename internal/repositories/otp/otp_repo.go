package repositoryotp

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type OTPRepository struct {
	Redis *redis.Client
}

func NewOTPRepository(redisClient *redis.Client) *OTPRepository {
	return &OTPRepository{Redis: redisClient}
}

const (
	otpRegisterKeyPrefix = "otp:register:"
	otpAttemptKeyPrefix  = "otp:attempt:"
	otpCooldownKeyPrefix = "otp:cooldown:"
	otpRateKeyPrefix     = "otp:rate:"
)

func (r *OTPRepository) SetOTP(ctx context.Context, email, hashed string, ttl time.Duration) error {
	return r.Redis.Set(ctx, fmt.Sprintf("%s%s", otpRegisterKeyPrefix, email), hashed, ttl).Err()
}

func (r *OTPRepository) GetOTP(ctx context.Context, email string) (string, error) {
	return r.Redis.Get(ctx, fmt.Sprintf("%s%s", otpRegisterKeyPrefix, email)).Result()
}

func (r *OTPRepository) DeleteOTP(ctx context.Context, email string) error {
	return r.Redis.Del(ctx, fmt.Sprintf("%s%s", otpRegisterKeyPrefix, email)).Err()
}

func (r *OTPRepository) IncrementAttempts(ctx context.Context, email string, ttl time.Duration) (int, error) {
	key := fmt.Sprintf("%s%s", otpAttemptKeyPrefix, email)
	count, err := r.Redis.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if count == 1 && ttl > 0 {
		_ = r.Redis.Expire(ctx, key, ttl).Err()
	}
	return int(count), nil
}

func (r *OTPRepository) ResetAttempts(ctx context.Context, email string) error {
	return r.Redis.Del(ctx, fmt.Sprintf("%s%s", otpAttemptKeyPrefix, email)).Err()
}

func (r *OTPRepository) SetCooldown(ctx context.Context, email string, ttl time.Duration) error {
	return r.Redis.Set(ctx, fmt.Sprintf("%s%s", otpCooldownKeyPrefix, email), "1", ttl).Err()
}

func (r *OTPRepository) GetCooldownTTL(ctx context.Context, email string) (time.Duration, error) {
	return r.Redis.TTL(ctx, fmt.Sprintf("%s%s", otpCooldownKeyPrefix, email)).Result()
}

func (r *OTPRepository) ClearCooldown(ctx context.Context, email string) error {
	return r.Redis.Del(ctx, fmt.Sprintf("%s%s", otpCooldownKeyPrefix, email)).Err()
}

func (r *OTPRepository) IncrementSendCount(ctx context.Context, email string, ttl time.Duration) (int, time.Duration, error) {
	key := fmt.Sprintf("%s%s", otpRateKeyPrefix, email)
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

func (r *OTPRepository) ClearSendCount(ctx context.Context, email string) error {
	return r.Redis.Del(ctx, fmt.Sprintf("%s%s", otpRateKeyPrefix, email)).Err()
}
