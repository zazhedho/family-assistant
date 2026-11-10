package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestFinanceMigrationIsSpaceScopedAndConstrained(t *testing.T) {
	upBytes, err := os.ReadFile("000013_create_finance_tables.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	tables := []string{"finance_accounts", "finance_categories", "finance_transactions", "finance_budgets", "finance_recurring"}
	for _, table := range tables {
		if !strings.Contains(up, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("up migration does not create %s", table)
		}
	}
	for _, required := range []string{
		"CHECK (amount > 0",
		"fk_finance_transactions_account_space",
		"fk_finance_transactions_to_account_space",
		"fk_finance_transactions_category_space",
		"fk_finance_budgets_category_space",
		"fk_finance_recurring_account_space",
		"fk_finance_recurring_category_space",
		"fk_finance_transactions_creator_space",
		"uq_finance_budgets_space_category_month",
		"uq_finance_accounts_space_id",
		"uq_finance_accounts_space_name",
		"uq_finance_categories_space_kind_name",
		"lower(btrim(name))",
		"uq_finance_categories_space_id",
		"'income', 'expense', 'transfer'",
		"'cash', 'bank', 'ewallet', 'savings', 'credit_card'",
		"account_id <> to_account_id",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"family_id", "hermes_profile_id"} {
		if strings.Contains(up, forbidden) {
			t.Errorf("contains %q", forbidden)
		}
	}

	downBytes, err := os.ReadFile("000013_create_finance_tables.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	last := -1
	for _, table := range []string{"finance_recurring", "finance_budgets", "finance_transactions", "finance_categories", "finance_accounts"} {
		i := strings.Index(down, "DROP TABLE IF EXISTS "+table)
		if i < 0 {
			t.Errorf("down migration does not drop %s", table)
			continue
		}
		if i < last {
			t.Errorf("down migration drops %s out of dependency order", table)
		}
		last = i
	}
}

func TestFinanceMigrationSeedsPermissions(t *testing.T) {
	upBytes, err := os.ReadFile("000013_create_finance_tables.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, required := range []string{
		"('33333333-3333-4333-8333-000000000029', 'list_finance'",
		"('33333333-3333-4333-8333-000000000030', 'create_finance'",
		"('33333333-3333-4333-8333-000000000031', 'update_finance'",
		"('33333333-3333-4333-8333-000000000032', 'delete_finance'",
		"('33333333-3333-4333-8333-000000000033', 'manage_finance'",
		"'finance', 'Finance', '/finance', 'bi-wallet2', 910",
		"ON CONFLICT (name) DO UPDATE",
		"ON CONFLICT DO NOTHING;",
		// owner/admin: all five; member: list, create, update; viewer: list only.
		"p.name IN ('list_finance', 'create_finance', 'update_finance', 'delete_finance', 'manage_finance') AND p.deleted_at IS NULL\nWHERE r.name IN ('space_owner', 'space_admin')",
		"p.name IN ('list_finance', 'create_finance', 'update_finance') AND p.deleted_at IS NULL\nWHERE r.name = 'space_member'",
		"p.name IN ('list_finance') AND p.deleted_at IS NULL\nWHERE r.name = 'space_viewer'",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("up migration missing %q", required)
		}
	}
	if got := strings.Count(up, "INSERT INTO role_permissions"); got != 3 {
		t.Errorf("role_permissions inserts: got %d, want 3", got)
	}

	downBytes, err := os.ReadFile("000013_create_finance_tables.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, id := range []string{"29", "30", "31", "32", "33"} {
		if !strings.Contains(down, "33333333-3333-4333-8333-0000000000"+id) {
			t.Errorf("down migration does not remove permission %s", id)
		}
	}
	if !strings.Contains(down, "22222222-2222-4222-8222-000000000006") {
		t.Errorf("down migration does not remove the finance menu item")
	}
}
