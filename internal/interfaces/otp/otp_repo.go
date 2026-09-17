package interfaceotp

import (
	"context"
	"time"
)

type RepoOTPInterface interface {
	SetOTP(ctx context.Context, email, hashed string, ttl time.Duration) error
	GetOTP(ctx context.Context, email string) (string, error)
	DeleteOTP(ctx context.Context, email string) error
	IncrementAttempts(ctx context.Context, email string, ttl time.Duration) (int, error)
	ResetAttempts(ctx context.Context, email string) error
	SetCooldown(ctx context.Context, email string, ttl time.Duration) error
	GetCooldownTTL(ctx context.Context, email string) (time.Duration, error)
	ClearCooldown(ctx context.Context, email string) error
	IncrementSendCount(ctx context.Context, email string, ttl time.Duration) (int, time.Duration, error)
	ClearSendCount(ctx context.Context, email string) error
}
