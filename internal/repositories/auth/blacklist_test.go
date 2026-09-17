package repositoryauth

import (
	"context"
	"errors"
	domainauth "starter-kit/internal/domain/auth"
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

func TestNewBlacklistRepoAndNilDBMethodPanics(t *testing.T) {
	repo := NewBlacklistRepo(nil)
	if repo == nil {
		t.Fatal("expected repo")
	}
	ctx := context.Background()
	if err := repo.Store(ctx, domainauth.Blacklist{Token: "token"}); !errors.Is(err, ErrBlacklistExpiryRequired) {
		t.Fatalf("expected expiry required error, got %v", err)
	}
	expectPanic(t, func() {
		_ = repo.Store(ctx, domainauth.Blacklist{Token: "token", ExpiresAt: time.Now().Add(time.Hour)})
	})
	expectPanic(t, func() { _, _ = repo.GetByToken(ctx, "token") })
	expectPanic(t, func() { _, _ = repo.ExistsByToken(ctx, "token") })
}
