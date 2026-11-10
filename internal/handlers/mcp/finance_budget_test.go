package mcp

import (
	"strings"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
)

func TestFinanceBudgetSetMapsInput(t *testing.T) {
	budget := &domainfinance.Budget{ID: "b-1", SpaceID: financeTestSpaceID, CategoryID: "cat-1", Month: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Amount: 200000000}
	service := &mcpFinanceServiceStub{budgetResult: budget}
	got, err := FinanceBudgetSet(mcpExternalContext(), financeResolver(), service, FinanceBudgetSetInput{Space: "Family", Category: "Makan", Month: "2026-11", Amount: "2000000"})
	if err != nil {
		t.Fatalf("finance_budget_set: %v", err)
	}
	assertFinanceActor(t, service)
	if service.budgetSet != (dto.BudgetSetInput{Space: financeTestSpaceID, Category: "Makan", Month: "2026-11", Amount: 200000000}) {
		t.Fatalf("input = %+v", service.budgetSet)
	}
	out := encodeToMap(t, got)
	if out["id"] != "b-1" || out["category_id"] != "cat-1" || out["month"] != "2026-11" || out["amount"] != "2000000" || out["space_id"] != financeTestSpaceID {
		t.Fatalf("output = %v", out)
	}
}

func TestFinanceBudgetListMapsInputAndEncodes(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceBudgetList(mcpExternalContext(), financeResolver(), service, FinanceBudgetListInput{Space: "Family", Month: "2026-11"})
	if err != nil {
		t.Fatalf("finance_budget_list: %v", err)
	}
	assertFinanceActor(t, service)
	if service.txSpaceID != financeTestSpaceID || service.budgetMonth != "2026-11" {
		t.Fatalf("space = %s, month = %s", service.txSpaceID, service.budgetMonth)
	}
	if mustJSON(t, got) != `{"budgets":[]}` {
		t.Fatalf("empty list encodes as %s", mustJSON(t, got))
	}
}

func TestFinanceBudgetListReturnsStatuses(t *testing.T) {
	service := &mcpFinanceServiceStub{budgetStatus: []dto.BudgetStatusView{{
		BudgetStatus: domainfinance.BudgetStatus{CategoryID: "cat-1", Budget: 10000000, Spent: 8500000, Remaining: 1500000, PercentUsed: 85}, CategoryName: "Makan",
	}}}
	got, err := FinanceBudgetList(mcpExternalContext(), financeResolver(), service, FinanceBudgetListInput{Space: "Family"})
	if err != nil || len(got.Budgets) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if service.budgetMonth != "" {
		t.Fatalf("month = %q, want blank (current month)", service.budgetMonth)
	}
	out := mustJSON(t, got)
	for _, want := range []string{`"category_name":"Makan"`, `"percent_used":85`, `"spent":"85000"`, `"remaining":"15000"`, `"budget":"100000"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s: %s", want, out)
		}
	}
}

func TestFinanceBudgetDeleteMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceBudgetDelete(mcpExternalContext(), financeResolver(), service, FinanceBudgetDeleteInput{Space: "Family", Category: "Makan", Month: "2026-11"})
	if err != nil || !got.Deleted {
		t.Fatalf("finance_budget_delete: %v, %+v", err, got)
	}
	assertFinanceActor(t, service)
	if service.budgetDelete != (dto.BudgetDeleteInput{Space: financeTestSpaceID, Category: "Makan", Month: "2026-11"}) {
		t.Fatalf("input = %+v", service.budgetDelete)
	}
}

func TestFinanceBudgetDescriptionsGuideTheBot(t *testing.T) {
	d := financeDescriptions(t)
	for _, name := range []string{"finance_budget_set", "finance_budget_delete"} {
		assertDescriptionHas(t, d, name, "YYYY-MM", "owner or admin", "ask an admin")
	}
	assertDescriptionHas(t, d, "finance_budget_set", "expense categories", "24 months")
	assertDescriptionHas(t, d, "finance_budget_list", "percent_used", "never compute", "YYYY-MM", "current month")
}
