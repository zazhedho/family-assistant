package permissioncache

import (
	"context"
	"testing"

	redismock "github.com/go-redis/redismock/v9"
)

func TestNewInvalidatorReturnsNilForNilClient(t *testing.T) {
	if got := NewInvalidator(nil); got != nil {
		t.Fatalf("expected nil invalidator, got %#v", got)
	}
}

func TestRedisInvalidatorDeletesUserKeys(t *testing.T) {
	client, mock := redismock.NewClientMock()
	invalidator := NewInvalidator(client)

	mock.ExpectDel("permission:user:user-1").SetVal(1)

	invalidator.DeleteUser(context.Background(), "user-1")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisInvalidatorDeletesAllKeys(t *testing.T) {
	client, mock := redismock.NewClientMock()
	invalidator := NewInvalidator(client)

	mock.ExpectScan(0, "permission:user:*", 100).SetVal([]string{"permission:user:user-1"}, 0)
	mock.ExpectDel("permission:user:user-1").SetVal(1)

	invalidator.DeleteAll(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
