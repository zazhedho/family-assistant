package mcp

import (
	"encoding/json"
	"testing"

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
