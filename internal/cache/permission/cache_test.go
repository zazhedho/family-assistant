package permissioncache

import (
	"context"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
)

func TestKey(t *testing.T) {
	if got := Key("user-1"); got != "permission:user:user-1" {
		t.Fatalf("Key() = %q", got)
	}
}

func TestTTLUsesDurationEnv(t *testing.T) {
	t.Setenv("PERMISSION_CACHE_TTL", "2m")
	t.Setenv("PERMISSION_CACHE_TTL_SECONDS", "5")

	if got := TTL(); got != 2*time.Minute {
		t.Fatalf("TTL() = %s", got)
	}
}

func TestTTLFallsBackToSecondsEnv(t *testing.T) {
	t.Setenv("PERMISSION_CACHE_TTL", "bad")
	t.Setenv("PERMISSION_CACHE_TTL_SECONDS", "30")

	if got := TTL(); got != 30*time.Second {
		t.Fatalf("TTL() = %s", got)
	}
}

func TestTTLFallsBackToFiveMinutes(t *testing.T) {
	t.Setenv("PERMISSION_CACHE_TTL", "bad")
	t.Setenv("PERMISSION_CACHE_TTL_SECONDS", "0")

	if got := TTL(); got != 5*time.Minute {
		t.Fatalf("TTL() = %s", got)
	}
}

func TestGetUserPermissionKeys(t *testing.T) {
	client, mock := redismock.NewClientMock()
	ctx := context.Background()

	mock.ExpectGet("permission:user:user-1").SetVal(`["users:list","users:create"]`)

	got, ok := GetUserPermissionKeys(ctx, client, "user-1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 2 || got[0] != "users:list" || got[1] != "users:create" {
		t.Fatalf("unexpected permission keys: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetUserPermissionKeysMissOnNilClient(t *testing.T) {
	got, ok := GetUserPermissionKeys(context.Background(), nil, "user-1")
	if ok {
		t.Fatalf("expected cache miss, got %#v", got)
	}
}

func TestGetUserPermissionKeysMissOnInvalidJSON(t *testing.T) {
	client, mock := redismock.NewClientMock()
	ctx := context.Background()

	mock.ExpectGet("permission:user:user-1").SetVal(`{`)

	got, ok := GetUserPermissionKeys(ctx, client, "user-1")
	if ok {
		t.Fatalf("expected cache miss, got %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetUserPermissionKeys(t *testing.T) {
	t.Setenv("PERMISSION_CACHE_TTL", "2m")

	client, mock := redismock.NewClientMock()
	ctx := context.Background()

	mock.ExpectSet("permission:user:user-1", `["users:list"]`, 2*time.Minute).SetVal("OK")

	SetUserPermissionKeys(ctx, client, "user-1", []string{"users:list"})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteUserPermissionKeys(t *testing.T) {
	client, mock := redismock.NewClientMock()
	ctx := context.Background()

	mock.ExpectDel("permission:user:user-1", "permission:user:user-2").SetVal(2)

	DeleteUserPermissionKeys(ctx, client, "user-1", "", " user-2 ")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAllUserPermissionKeys(t *testing.T) {
	client, mock := redismock.NewClientMock()
	ctx := context.Background()

	mock.ExpectScan(0, "permission:user:*", 100).SetVal([]string{"permission:user:user-1"}, 0)
	mock.ExpectDel("permission:user:user-1").SetVal(1)

	DeleteAllUserPermissionKeys(ctx, client)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
