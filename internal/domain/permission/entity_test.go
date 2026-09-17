package domainpermission

import "testing"

func TestTableName(t *testing.T) {
	if got := (Permission{}).TableName(); got != "permissions" {
		t.Fatalf("expected permissions, got %q", got)
	}
}
