package domainauth

import "testing"

func TestTableName(t *testing.T) {
	if got := (Blacklist{}).TableName(); got != "blacklist" {
		t.Fatalf("expected blacklist, got %q", got)
	}
}
