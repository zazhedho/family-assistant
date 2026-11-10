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
		"uq_finance_categories_space_id",
		"'income', 'expense', 'transfer'",
		"'cash', 'bank', 'ewallet', 'savings', 'credit_card'",
		"account_id <> to_account_id",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"family_id", "hermes_profile_id", "INSERT INTO permissions"} {
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
