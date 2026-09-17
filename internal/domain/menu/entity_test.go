package domainmenu

import "testing"

func TestTableName(t *testing.T) {
	if got := (MenuItem{}).TableName(); got != "menu_items" {
		t.Fatalf("expected menu_items, got %q", got)
	}
}
