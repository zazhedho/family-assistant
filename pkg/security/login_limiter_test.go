package security

import (
	"context"
	"errors"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
)

func TestNewRedisLoginLimiterReturnsNilForInvalidConfig(t *testing.T) {
	if got := NewRedisLoginLimiter(nil, 5, time.Minute, time.Minute); got != nil {
		t.Fatalf("expected nil limiter for nil client, got %#v", got)
	}
	if got := NewRedisLoginLimiter(nil, 0, time.Minute, time.Minute); got != nil {
		t.Fatalf("expected nil limiter for zero limit, got %#v", got)
	}
}

func TestRedisLoginLimiterKeyBuilders(t *testing.T) {
	limiter := &redisLoginLimiter{}
	if got := limiter.attemptKey("jane@example.com"); got != "login_attempts:jane@example.com" {
		t.Fatalf("unexpected attempt key: %q", got)
	}
	if got := limiter.blockKey("jane@example.com"); got != "login_block:jane@example.com" {
		t.Fatalf("unexpected block key: %q", got)
	}
}

func TestRedisLoginLimiterUsesRedisForBlockingAndReset(t *testing.T) {
	client, mock := redismock.NewClientMock()
	limiter := NewRedisLoginLimiter(client, 3, time.Minute, 5*time.Minute)
	ctx := context.Background()

	mock.ExpectTTL("login_block:client-1").SetVal(30 * time.Second)
	blocked, ttl, err := limiter.IsBlocked(ctx, "client-1")
	if err != nil || !blocked || ttl != 30*time.Second {
		t.Fatalf("is blocked: blocked=%v ttl=%v err=%v", blocked, ttl, err)
	}

	mock.ExpectDel("login_attempts:client-1").SetVal(1)
	mock.ExpectDel("login_block:client-1").SetVal(1)
	if err := limiter.Reset(ctx, "client-1"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisLoginLimiterRegisterFailure(t *testing.T) {
	client, mock := redismock.NewClientMock()
	limiter := NewRedisLoginLimiter(client, 3, time.Minute, 5*time.Minute)
	ctx := context.Background()

	mock.ExpectIncr("login_attempts:client-1").SetVal(1)
	mock.ExpectExpire("login_attempts:client-1", time.Minute).SetVal(true)
	mock.ExpectTTL("login_attempts:client-1").SetVal(time.Minute)
	blocked, ttl, err := limiter.RegisterFailure(ctx, "client-1")
	if err != nil || blocked || ttl != time.Minute {
		t.Fatalf("first failure: blocked=%v ttl=%v err=%v", blocked, ttl, err)
	}

	mock.ExpectIncr("login_attempts:client-1").SetVal(3)
	mock.ExpectSet("login_block:client-1", "1", 5*time.Minute).SetVal("OK")
	mock.ExpectDel("login_attempts:client-1").SetVal(1)
	blocked, ttl, err = limiter.RegisterFailure(ctx, "client-1")
	if err != nil || !blocked || ttl != 5*time.Minute {
		t.Fatalf("limit failure: blocked=%v ttl=%v err=%v", blocked, ttl, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisLoginLimiterNilReceiverAndRedisErrors(t *testing.T) {
	ctx := context.Background()
	var nilLimiter *redisLoginLimiter
	if blocked, ttl, err := nilLimiter.IsBlocked(ctx, "client-1"); err != nil || blocked || ttl != 0 {
		t.Fatalf("nil is blocked: blocked=%v ttl=%v err=%v", blocked, ttl, err)
	}
	if blocked, ttl, err := nilLimiter.RegisterFailure(ctx, "client-1"); err != nil || blocked || ttl != 0 {
		t.Fatalf("nil register failure: blocked=%v ttl=%v err=%v", blocked, ttl, err)
	}
	if err := nilLimiter.Reset(ctx, "client-1"); err != nil {
		t.Fatalf("nil reset: %v", err)
	}

	client, mock := redismock.NewClientMock()
	limiter := NewRedisLoginLimiter(client, 2, time.Minute, 5*time.Minute)
	redisErr := errors.New("redis down")

	mock.ExpectTTL("login_block:client-1").SetErr(redisErr)
	if _, _, err := limiter.IsBlocked(ctx, "client-1"); err == nil {
		t.Fatal("expected ttl error")
	}

	mock.ExpectTTL("login_block:client-1").SetVal(0)
	if blocked, ttl, err := limiter.IsBlocked(ctx, "client-1"); err != nil || blocked || ttl != 0 {
		t.Fatalf("expected not blocked, blocked=%v ttl=%v err=%v", blocked, ttl, err)
	}

	mock.ExpectIncr("login_attempts:client-1").SetErr(redisErr)
	if _, _, err := limiter.RegisterFailure(ctx, "client-1"); err == nil {
		t.Fatal("expected incr error")
	}

	mock.ExpectIncr("login_attempts:client-1").SetVal(1)
	mock.ExpectExpire("login_attempts:client-1", time.Minute).SetErr(redisErr)
	if _, _, err := limiter.RegisterFailure(ctx, "client-1"); err == nil {
		t.Fatal("expected expire error")
	}

	mock.ExpectIncr("login_attempts:client-1").SetVal(2)
	mock.ExpectSet("login_block:client-1", "1", 5*time.Minute).SetErr(redisErr)
	if _, _, err := limiter.RegisterFailure(ctx, "client-1"); err == nil {
		t.Fatal("expected set block error")
	}

	mock.ExpectDel("login_attempts:client-1").SetErr(redisErr)
	if err := limiter.Reset(ctx, "client-1"); err == nil {
		t.Fatal("expected reset attempts error")
	}

	mock.ExpectDel("login_attempts:client-1").SetVal(0)
	mock.ExpectDel("login_block:client-1").SetErr(redisErr)
	if err := limiter.Reset(ctx, "client-1"); err == nil {
		t.Fatal("expected reset block error")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
