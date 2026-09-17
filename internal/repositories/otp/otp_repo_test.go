package repositoryotp

import (
	"context"
	"testing"
	"time"
)

func expectPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}

func TestNewOTPRepositoryAndNilRedisMethodPanics(t *testing.T) {
	repo := NewOTPRepository(nil)
	if repo == nil {
		t.Fatal("expected repo")
	}
	ctx := context.Background()
	expectPanic(t, func() { _ = repo.SetOTP(ctx, "a@example.com", "hash", time.Minute) })
	expectPanic(t, func() { _, _ = repo.GetOTP(ctx, "a@example.com") })
	expectPanic(t, func() { _ = repo.DeleteOTP(ctx, "a@example.com") })
	expectPanic(t, func() { _, _ = repo.IncrementAttempts(ctx, "a@example.com", time.Minute) })
	expectPanic(t, func() { _ = repo.ResetAttempts(ctx, "a@example.com") })
	expectPanic(t, func() { _ = repo.SetCooldown(ctx, "a@example.com", time.Minute) })
	expectPanic(t, func() { _, _ = repo.GetCooldownTTL(ctx, "a@example.com") })
	expectPanic(t, func() { _ = repo.ClearCooldown(ctx, "a@example.com") })
	expectPanic(t, func() { _, _, _ = repo.IncrementSendCount(ctx, "a@example.com", time.Minute) })
	expectPanic(t, func() { _ = repo.ClearSendCount(ctx, "a@example.com") })
}
