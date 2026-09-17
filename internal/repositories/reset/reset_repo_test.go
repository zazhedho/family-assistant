package repositoryreset

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

func TestNewPasswordResetRepositoryAndNilRedisMethodPanics(t *testing.T) {
	repo := NewPasswordResetRepository(nil)
	if repo == nil {
		t.Fatal("expected repo")
	}
	ctx := context.Background()
	expectPanic(t, func() { _ = repo.SetToken(ctx, "hash", "a@example.com", time.Minute) })
	expectPanic(t, func() { _, _ = repo.GetEmailByToken(ctx, "hash") })
	expectPanic(t, func() { _ = repo.DeleteToken(ctx, "hash") })
	expectPanic(t, func() { _ = repo.SetCooldown(ctx, "a@example.com", time.Minute) })
	expectPanic(t, func() { _, _ = repo.GetCooldownTTL(ctx, "a@example.com") })
	expectPanic(t, func() { _ = repo.ClearCooldown(ctx, "a@example.com") })
	expectPanic(t, func() { _, _, _ = repo.IncrementSendCount(ctx, "a@example.com", time.Minute) })
	expectPanic(t, func() { _ = repo.ClearSendCount(ctx, "a@example.com") })
}
