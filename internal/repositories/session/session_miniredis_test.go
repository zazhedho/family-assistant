package repositorysession

import (
	"context"
	"encoding/json"
	"fmt"
	domainsession "starter-kit/internal/domain/session"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

func matchRedisCommandKey(expected, actual []any) error {
	if len(expected) > 1 && expected[1] != actual[1] {
		return fmt.Errorf("key mismatch: expected %v, got %v", expected[1], actual[1])
	}
	if len(expected) > 2 {
		switch expected[0] {
		case "sadd", "srem":
			if expected[2] != actual[2] {
				return fmt.Errorf("member mismatch: expected %v, got %v", expected[2], actual[2])
			}
		}
	}
	return nil
}

func TestSessionRepositoryWithRedisMock(t *testing.T) {
	client, mock := redismock.NewClientMock()
	redisExpect := mock.CustomMatch(matchRedisCommandKey)
	repo := NewSessionRepository(client)
	ctx := context.Background()
	now := time.Now()
	expiresAt := now.Add(time.Hour)
	session := &domainsession.Session{
		SessionID:    "session-1",
		UserID:       "user-1",
		Username:     "Jane",
		Email:        "jane@example.com",
		Role:         "viewer",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		DeviceInfo:   "Mac",
		IP:           "127.0.0.1",
		LoginAt:      now,
		LastActivity: now,
		ExpiresAt:    expiresAt,
	}
	sessionData, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}

	redisExpect.ExpectSet("session:session-1", "", time.Hour).SetVal("OK")
	redisExpect.ExpectSAdd("user_sessions:user-1", "session-1").SetVal(1)
	redisExpect.ExpectExpire("user_sessions:user-1", time.Hour).SetVal(true)
	redisExpect.ExpectSet("access_token_session:access-1", "session-1", time.Hour).SetVal("OK")
	redisExpect.ExpectSet("refresh_token_session:refresh-1", "session-1", time.Hour).SetVal("OK")
	mock.ExpectGet("session:session-1").SetVal(string(sessionData))
	mock.ExpectGet("access_token_session:access-1").SetVal("session-1")
	mock.ExpectGet("session:session-1").SetVal(string(sessionData))
	mock.ExpectGet("refresh_token_session:refresh-1").SetVal("session-1")
	mock.ExpectGet("session:session-1").SetVal(string(sessionData))
	mock.ExpectSMembers("user_sessions:user-1").SetVal([]string{"session-1"})
	mock.ExpectGet("session:session-1").SetVal(string(sessionData))
	mock.ExpectGet("session:session-1").SetVal(string(sessionData))
	mock.ExpectTTL("session:session-1").SetVal(time.Hour)
	redisExpect.ExpectSet("session:session-1", "", time.Hour).SetVal("OK")
	mock.ExpectGet("session:session-1").SetVal(string(sessionData))
	mock.ExpectDel("access_token_session:access-1").SetVal(1)
	mock.ExpectDel("refresh_token_session:refresh-1").SetVal(1)
	redisExpect.ExpectSet("session:session-1", "", time.Hour).SetVal("OK")
	redisExpect.ExpectSet("access_token_session:access-2", "session-1", time.Hour).SetVal("OK")
	redisExpect.ExpectSet("refresh_token_session:refresh-2", "session-1", time.Hour).SetVal("OK")
	redisExpect.ExpectExpire("user_sessions:user-1", time.Hour).SetVal(true)
	rotatedSession := *session
	rotatedSession.AccessToken = "access-2"
	rotatedSession.RefreshToken = "refresh-2"
	rotatedSession.ExpiresAt = expiresAt
	rotatedData, err := json.Marshal(rotatedSession)
	if err != nil {
		t.Fatalf("marshal rotated session: %v", err)
	}
	mock.ExpectGet("access_token_session:access-2").SetVal("session-1")
	mock.ExpectGet("session:session-1").SetVal(string(rotatedData))
	redisExpect.ExpectExpire("session:session-1", time.Minute).SetVal(true)
	mock.ExpectGet("session:session-1").SetVal(string(rotatedData))
	mock.ExpectDel("session:session-1").SetVal(1)
	mock.ExpectSRem("user_sessions:user-1", "session-1").SetVal(1)
	mock.ExpectDel("access_token_session:access-2").SetVal(1)
	mock.ExpectDel("refresh_token_session:refresh-2").SetVal(1)
	mock.ExpectGet("session:session-1").RedisNil()

	if err := repo.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if got, err := repo.GetBySessionID(ctx, "session-1"); err != nil || got.UserID != "user-1" {
		t.Fatalf("get session: got=%+v err=%v", got, err)
	}
	if got, err := repo.GetByToken(ctx, "access-1"); err != nil || got.SessionID != "session-1" {
		t.Fatalf("get by token: got=%+v err=%v", got, err)
	}
	if got, err := repo.GetByRefreshToken(ctx, "refresh-1"); err != nil || got.SessionID != "session-1" {
		t.Fatalf("get by refresh: got=%+v err=%v", got, err)
	}
	if sessions, err := repo.GetByUserID(ctx, "user-1"); err != nil || len(sessions) != 1 {
		t.Fatalf("get by user: sessions=%+v err=%v", sessions, err)
	}
	if err := repo.UpdateActivity(ctx, "session-1"); err != nil {
		t.Fatalf("update activity: %v", err)
	}
	if err := repo.RotateTokens(ctx, "session-1", "access-2", "refresh-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("rotate tokens: %v", err)
	}
	if got, err := repo.GetByToken(ctx, "access-2"); err != nil || got.AccessToken != "access-2" {
		t.Fatalf("get rotated token: got=%+v err=%v", got, err)
	}
	if err := repo.SetExpiration(ctx, "session-1", time.Minute); err != nil {
		t.Fatalf("set expiration: %v", err)
	}
	if err := repo.Delete(ctx, "session-1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := repo.GetBySessionID(ctx, "session-1"); err == nil {
		t.Fatal("expected deleted session to be missing")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestSessionRepositoryRejectsExpiredSessionOnCreateAndDeleteByUser(t *testing.T) {
	client, mock := redismock.NewClientMock()
	repo := NewSessionRepository(client)
	ctx := context.Background()
	if err := repo.Create(ctx, &domainsession.Session{SessionID: "expired", ExpiresAt: time.Now().Add(-time.Minute)}); err == nil {
		t.Fatal("expected expired session create error")
	}
	mock.ExpectSMembers("user_sessions:missing-user").SetVal([]string{})
	if err := repo.DeleteByUserID(ctx, "missing-user"); err != nil {
		t.Fatalf("delete by user should tolerate missing set, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestSessionRepositoryGetBySessionIDPropagatesRedisError(t *testing.T) {
	client, mock := redismock.NewClientMock()
	repo := NewSessionRepository(client)
	mock.ExpectGet("session:session-1").SetErr(redis.TxFailedErr)

	if _, err := repo.GetBySessionID(context.Background(), "session-1"); err == nil {
		t.Fatal("expected redis error")
	}
}
