package repositoryreset

import (
	"context"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
)

func TestPasswordResetRepositoryWithRedisMock(t *testing.T) {
	client, mock := redismock.NewClientMock()
	repo := NewPasswordResetRepository(client)
	ctx := context.Background()
	email := "jane@example.com"

	mock.ExpectSet("reset:token:hash", email, time.Minute).SetVal("OK")
	mock.ExpectGet("reset:token:hash").SetVal(email)
	mock.ExpectSet("reset:cooldown:"+email, "1", time.Minute).SetVal("OK")
	mock.ExpectTTL("reset:cooldown:" + email).SetVal(time.Minute)
	mock.ExpectDel("reset:cooldown:" + email).SetVal(1)
	mock.ExpectIncr("reset:rate:" + email).SetVal(1)
	mock.ExpectExpire("reset:rate:"+email, time.Minute).SetVal(true)
	mock.ExpectTTL("reset:rate:" + email).SetVal(time.Minute)
	mock.ExpectDel("reset:rate:" + email).SetVal(1)
	mock.ExpectDel("reset:token:hash").SetVal(1)

	if err := repo.SetToken(ctx, "hash", email, time.Minute); err != nil {
		t.Fatalf("set token: %v", err)
	}
	if got, err := repo.GetEmailByToken(ctx, "hash"); err != nil || got != "jane@example.com" {
		t.Fatalf("get token: got=%q err=%v", got, err)
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

	if err := repo.DeleteToken(ctx, "hash"); err != nil {
		t.Fatalf("delete token: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
