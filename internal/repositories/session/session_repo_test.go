package repositorysession

import (
	"context"
	domainsession "starter-kit/internal/domain/session"
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

func TestNewSessionRepositoryAndNilRedisMethodPanics(t *testing.T) {
	repo := NewSessionRepository(nil)
	if repo == nil {
		t.Fatal("expected repo")
	}

	ctx := context.Background()
	session := &domainsession.Session{
		SessionID:    "session-1",
		UserID:       "user-1",
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	expectPanic(t, func() { _ = repo.Create(ctx, session) })
	expectPanic(t, func() { _, _ = repo.GetBySessionID(ctx, "session-1") })
	expectPanic(t, func() { _, _ = repo.GetByUserID(ctx, "user-1") })
	expectPanic(t, func() { _, _ = repo.GetByToken(ctx, "access") })
	expectPanic(t, func() { _, _ = repo.GetByRefreshToken(ctx, "refresh") })
	expectPanic(t, func() { _ = repo.UpdateActivity(ctx, "session-1") })
	expectPanic(t, func() { _ = repo.Delete(ctx, "session-1") })
	expectPanic(t, func() { _ = repo.DeleteByUserID(ctx, "user-1") })
	expectPanic(t, func() {
		_ = repo.RotateTokens(ctx, "session-1", "new-access", "new-refresh", time.Now().Add(time.Hour))
	})
	expectPanic(t, func() { _ = repo.SetExpiration(ctx, "session-1", time.Hour) })

	if err := repo.DeleteExpired(ctx); err != nil {
		t.Fatalf("expected delete expired no-op, got %v", err)
	}
}
