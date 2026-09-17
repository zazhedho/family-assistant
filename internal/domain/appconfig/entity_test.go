package domainappconfig

import "testing"

func TestTableName(t *testing.T) {
	if got := (AppConfig{}).TableName(); got != "app_configs" {
		t.Fatalf("expected app_configs, got %q", got)
	}
}
