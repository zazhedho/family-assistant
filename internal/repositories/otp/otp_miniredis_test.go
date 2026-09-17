package repositoryotp

import (
	"context"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
)

func TestOTPRepositoryWithRedisMock(t *testing.T) {
	client, mock := redismock.NewClientMock()
	repo := NewOTPRepository(client)
	ctx := context.Background()
	email := "jane@example.com"

	mock.ExpectSet("otp:register:"+email, "hashed", time.Minute).SetVal("OK")
	mock.ExpectGet("otp:register:" + email).SetVal("hashed")
	mock.ExpectIncr("otp:attempt:" + email).SetVal(1)
	mock.ExpectExpire("otp:attempt:"+email, time.Minute).SetVal(true)
	mock.ExpectDel("otp:attempt:" + email).SetVal(1)
	mock.ExpectSet("otp:cooldown:"+email, "1", time.Minute).SetVal("OK")
	mock.ExpectTTL("otp:cooldown:" + email).SetVal(time.Minute)
	mock.ExpectDel("otp:cooldown:" + email).SetVal(1)
	mock.ExpectIncr("otp:rate:" + email).SetVal(1)
	mock.ExpectExpire("otp:rate:"+email, time.Minute).SetVal(true)
	mock.ExpectTTL("otp:rate:" + email).SetVal(time.Minute)
	mock.ExpectDel("otp:rate:" + email).SetVal(1)
	mock.ExpectDel("otp:register:" + email).SetVal(1)

	if err := repo.SetOTP(ctx, email, "hashed", time.Minute); err != nil {
		t.Fatalf("set otp: %v", err)
	}
	if got, err := repo.GetOTP(ctx, email); err != nil || got != "hashed" {
		t.Fatalf("get otp: got=%q err=%v", got, err)
	}

	attempts, err := repo.IncrementAttempts(ctx, email, time.Minute)
	if err != nil || attempts != 1 {
		t.Fatalf("increment attempts: got=%d err=%v", attempts, err)
	}
	if err := repo.ResetAttempts(ctx, email); err != nil {
		t.Fatalf("reset attempts: %v", err)
	}

	if err := repo.SetCooldown(ctx, email, time.Minute); err != nil {
		t.Fatalf("set cooldown: %v", err)
	}
	if ttl, err := repo.GetCooldownTTL(ctx, email); err != nil || ttl <= 0 {
		t.Fatalf("cooldown ttl: ttl=%v err=%v", ttl, err)
	}
	if err := repo.ClearCooldown(ctx, email); err != nil {
		t.Fatalf("clear cooldown: %v", err)
	}

	count, retryAfter, err := repo.IncrementSendCount(ctx, email, time.Minute)
	if err != nil || count != 1 || retryAfter <= 0 {
		t.Fatalf("increment send count: count=%d retry=%v err=%v", count, retryAfter, err)
	}
	if err := repo.ClearSendCount(ctx, email); err != nil {
		t.Fatalf("clear send count: %v", err)
	}

	if err := repo.DeleteOTP(ctx, email); err != nil {
		t.Fatalf("delete otp: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
