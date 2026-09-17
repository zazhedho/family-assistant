package database

import (
	"strings"
	"testing"
)

func TestGetAndCloseRedisWhenUnset(t *testing.T) {
	RedisClient = nil
	if got := GetRedisClient(); got != nil {
		t.Fatalf("expected nil redis client, got %#v", got)
	}
	if err := CloseRedis(); err != nil {
		t.Fatalf("expected nil close error, got %v", err)
	}
}

func TestInitRedisReturnsPingError(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://127.0.0.1:1/0")
	RedisClient = nil

	client, err := InitRedis()
	if err == nil {
		if client != nil {
			_ = client.Close()
		}
		t.Fatal("expected redis connection error")
	}
	if client != nil {
		t.Fatalf("expected nil client on error, got %#v", client)
	}
}

func TestConnDbReturnsOpenErrorForInvalidDSN(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://%")

	db, sqlDB, err := ConnDb()
	if err == nil {
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		t.Fatalf("expected invalid dsn error, db=%#v", db)
	}
	if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse-like error, got %v", err)
	}
}
