package domainuser

import "testing"

func TestTableName(t *testing.T) {
	if got := (Users{}).TableName(); got != "users" {
		t.Fatalf("expected users, got %q", got)
	}
}
