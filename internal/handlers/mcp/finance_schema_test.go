package mcp

import (
	"encoding/json"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"

	"github.com/google/jsonschema-go/jsonschema"
)

// validateAgainstSchema checks that the encoded output satisfies the output
// schema the MCP SDK infers for the type, which it enforces on every call.
func validateAgainstSchema[T any](t *testing.T, value T) {
	t.Helper()
	schema, err := jsonschema.For[T](nil)
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
	budget := domainfinance.BudgetStatus{CategoryID: "cat-1", Budget: 100, Spent: 100, Remaining: 0, PercentUsed: 100}
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
	account := &domainfinance.Account{ID: "acc-1", SpaceID: "sp-1", Name: "BCA", Type: "bank", OpeningBalance: 1500000, ArchivedAt: &archived, CreatedAt: now, UpdatedAt: now}
	category := &domainfinance.Category{ID: "cat-1", SpaceID: "sp-1", Name: "Makan", Kind: "expense", ArchivedAt: &archived, CreatedAt: now, UpdatedAt: now}
	validateAgainstSchema(t, account)
	validateAgainstSchema(t, category)
	validateAgainstSchema(t, FinanceAccountListOutput{Accounts: []domainfinance.Account{*account}})
	validateAgainstSchema(t, FinanceCategoryListOutput{Categories: []domainfinance.Category{*category}})
	validateAgainstSchema(t, FinanceBudgetOutput{ID: "b-1", SpaceID: "sp-1", CategoryID: "cat-1", Month: "2026-11", Amount: 2000000, CreatedAt: now, UpdatedAt: now})
	validateAgainstSchema(t, FinanceBudgetListOutput{Budgets: []dto.BudgetStatusView{{
		BudgetStatus: domainfinance.BudgetStatus{CategoryID: "cat-1", Budget: 2000000, Spent: 500000, Remaining: 1500000, PercentUsed: 25}, CategoryName: "Makan",
	}}})
}
