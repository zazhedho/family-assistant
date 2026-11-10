package mcp

import (
	"encoding/json"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
)

// validateAgainstSchema checks that the encoded output satisfies the output
// schema the finance tools publish for the type (money as a decimal string),
// which the MCP SDK enforces on every call.
func validateAgainstSchema[T any](t *testing.T, value T) {
	t.Helper()
	schema, err := financeOutputSchema[T]()
	if err != nil {
		t.Fatalf("infer schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve schema: %v", err)
	}
	var instance any
	if err := json.Unmarshal([]byte(mustJSON(t, value)), &instance); err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(instance); err != nil {
		t.Fatalf("output violates its own schema: %v\n%s", err, mustJSON(t, value))
	}
}

func TestFinanceOutputsSatisfyTheirSchemas(t *testing.T) {
	tx := financeTestTransaction()
	budget := domainfinance.BudgetStatus{CategoryID: "cat-1", Budget: 1, Spent: 1, Remaining: 0, PercentUsed: 100}
	validateAgainstSchema(t, *financeTransactionResult(&dto.TransactionResult{Transaction: tx, PossibleDuplicate: &tx, BudgetStatus: &budget}))
	validateAgainstSchema(t, *financeTransactionResult(&dto.TransactionResult{Transaction: tx}))
	validateAgainstSchema(t, FinanceTransactionListOutput{Transactions: []FinanceTransactionOutput{financeTransactionOutput(tx)}})
	validateAgainstSchema(t, financeRecurringOutput(*financeTestRecurring()))
	validateAgainstSchema(t, FinanceRecurringListOutput{Recurring: []FinanceRecurringOutput{}})
	validateAgainstSchema(t, FinanceDeleteOutput{Deleted: true})
	validateAgainstSchema(t, FinanceBudgetListOutput{Budgets: []dto.BudgetStatusView{}})
	validateAgainstSchema(t, FinanceSummaryOutput{ByCategory: []domainfinance.CategoryTotal{}, ByAccount: []domainfinance.AccountTotal{}, TopMerchants: []domainfinance.MerchantTotal{}})
	validateAgainstSchema(t, FinanceBalancesOutput{Accounts: []domainfinance.AccountBalance{}})
}

func TestFinancePopulatedOutputsSatisfyTheirSchemas(t *testing.T) {
	now := time.Date(2026, 11, 10, 7, 30, 0, 0, time.UTC)
	archived := now.Add(time.Hour)
	account := &domainfinance.Account{ID: "acc-1", SpaceID: "sp-1", Name: "BCA", Type: "bank", OpeningBalance: 100542244, ArchivedAt: &archived, CreatedAt: now, UpdatedAt: now}
	category := &domainfinance.Category{ID: "cat-1", SpaceID: "sp-1", Name: "Makan", Kind: "expense", ArchivedAt: &archived, CreatedAt: now, UpdatedAt: now}
	validateAgainstSchema(t, account)
	validateAgainstSchema(t, category)
	validateAgainstSchema(t, FinanceAccountListOutput{Accounts: []domainfinance.Account{*account}})
	validateAgainstSchema(t, FinanceCategoryListOutput{Categories: []domainfinance.Category{*category}})
	validateAgainstSchema(t, FinanceBudgetOutput{ID: "b-1", SpaceID: "sp-1", CategoryID: "cat-1", Month: "2026-11", Amount: 100542244, CreatedAt: now, UpdatedAt: now})
	validateAgainstSchema(t, FinanceBudgetListOutput{Budgets: []dto.BudgetStatusView{{
		BudgetStatus: domainfinance.BudgetStatus{CategoryID: "cat-1", Budget: 2000000, Spent: 500000, Remaining: 1500000, PercentUsed: 25}, CategoryName: "Makan",
	}}})
}

// Every money-carrying output with populated decimal values (odd sen, negative,
// zero, the bound) must satisfy its published schema; the SDK validates this on
// every call.
func TestFinanceMoneyOutputsWithDecimalsSatisfyTheirSchemas(t *testing.T) {
	now := time.Date(2026, 11, 10, 7, 30, 0, 0, time.UTC)
	for _, amount := range []domainfinance.Money{0, 1, 10, 50, 100, 100542244, -1, -50, -100542244, domainfinance.MaxAmount, -domainfinance.MaxAmount} {
		tx := financeTestTransaction()
		tx.Amount = amount
		budget := domainfinance.NewBudgetStatus("cat-1", domainfinance.MaxAmount, amount)
		validateAgainstSchema(t, *financeTransactionResult(&dto.TransactionResult{Transaction: tx, PossibleDuplicate: &tx, BudgetStatus: &budget}))
		validateAgainstSchema(t, FinanceBalancesOutput{Accounts: []domainfinance.AccountBalance{{AccountID: "a", Balance: amount}}, Total: amount})
		validateAgainstSchema(t, FinanceSummaryOutput{
			Income: amount, Expense: amount, Net: amount, PreviousIncome: amount, PreviousExpense: amount,
			ByCategory:   []domainfinance.CategoryTotal{{CategoryID: "c", Total: amount}},
			ByAccount:    []domainfinance.AccountTotal{{AccountID: "a", Income: amount, Expense: amount}},
			TopMerchants: []domainfinance.MerchantTotal{{Merchant: "m", Total: amount, Count: 1}},
		})
		validateAgainstSchema(t, FinanceBudgetOutput{ID: "b", SpaceID: "s", CategoryID: "c", Month: "2026-11", Amount: amount, CreatedAt: now, UpdatedAt: now})
		validateAgainstSchema(t, FinanceAccountListOutput{Accounts: []domainfinance.Account{{ID: "a", OpeningBalance: amount, CreatedAt: now, UpdatedAt: now}}})
		recurring := *financeTestRecurring()
		recurring.Amount = amount
		validateAgainstSchema(t, financeRecurringOutput(recurring))
	}
}

// The schema must reject what the encoder never produces, so the pattern is real.
func TestFinanceMoneyOutputSchemaRejectsNonDecimalStrings(t *testing.T) {
	schema, err := financeOutputSchema[FinanceBalancesOutput]()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, total := range []any{"1,5", "1e3", "1.5", " 5", 25000.0, nil} {
		instance := map[string]any{"accounts": []any{}, "total": total}
		if err := resolved.Validate(instance); err == nil {
			t.Errorf("total %v (%T) validated, want rejection", total, total)
		}
	}
	for _, total := range []string{"0", "25000", "1005422.44", "-0.50"} {
		if err := resolved.Validate(map[string]any{"accounts": []any{}, "total": total}); err != nil {
			t.Errorf("total %q rejected: %v", total, err)
		}
	}
}
