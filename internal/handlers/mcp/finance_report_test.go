package mcp

import (
	"strings"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
)

func TestFinanceSummaryMapsInputAndRendersWIB(t *testing.T) {
	report := &dto.SummaryReport{
		Period: "custom", From: time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC),
		Income: 5000000, Expense: 1250000, Net: 3750000, PreviousIncome: 4000000, PreviousExpense: 900000,
		ByCategory:   []domainfinance.CategoryTotal{{CategoryID: "cat-1", Name: "Makan", Total: 1250000}},
		ByAccount:    []domainfinance.AccountTotal{{AccountID: "acc-1", Name: "BCA", Income: 5000000, Expense: 1250000}},
		TopMerchants: []domainfinance.MerchantTotal{{Merchant: "Warung", Total: 250000, Count: 4}},
	}
	service := &mcpFinanceServiceStub{summary: report}
	got, err := FinanceSummary(mcpExternalContext(), financeResolver(), service, FinanceSummaryInput{
		Space: "Family", Period: "custom", From: "2026-11-10T00:00:00+07:00", To: "2026-12-01T00:00:00+07:00",
	})
	if err != nil {
		t.Fatalf("finance_summary: %v", err)
	}
	assertFinanceActor(t, service)
	in := service.summaryIn
	if in.Space != financeTestSpaceID || in.Period != "custom" || in.From == nil || !in.From.Equal(report.From) || in.To == nil || !in.To.Equal(report.To) {
		t.Fatalf("input = %+v", in)
	}
	out := encodeToMap(t, got)
	if out["from"] != "2026-11-10T00:00:00+07:00" || out["to"] != "2026-12-01T00:00:00+07:00" || out["period"] != "custom" ||
		out["income"] != float64(5000000) || out["expense"] != float64(1250000) || out["net"] != float64(3750000) ||
		out["previous_income"] != float64(4000000) || out["previous_expense"] != float64(900000) {
		t.Fatalf("output = %v", out)
	}
	encoded := mustJSON(t, got)
	for _, want := range []string{`"by_category":[{"category_id":"cat-1","name":"Makan","total":1250000}]`, `"top_merchants":[{"merchant":"Warung","total":250000,"count":4}]`, `"by_account":[{"account_id":"acc-1"`} {
		if !strings.Contains(encoded, want) {
			t.Errorf("output lacks %s: %s", want, encoded)
		}
	}
}

func TestFinanceSummaryNilBoundsAndEmptyListsEncodeAsArrays(t *testing.T) {
	service := &mcpFinanceServiceStub{summary: &dto.SummaryReport{Period: "month"}}
	got, err := FinanceSummary(mcpExternalContext(), financeResolver(), service, FinanceSummaryInput{Space: "Family"})
	if err != nil {
		t.Fatalf("finance_summary: %v", err)
	}
	if service.summaryIn.From != nil || service.summaryIn.To != nil || service.summaryIn.Period != "" {
		t.Fatalf("input = %+v", service.summaryIn)
	}
	encoded := mustJSON(t, got)
	for _, want := range []string{`"by_category":[]`, `"by_account":[]`, `"top_merchants":[]`} {
		if !strings.Contains(encoded, want) {
			t.Errorf("output lacks %s: %s", want, encoded)
		}
	}
}

func TestFinanceSummaryRejectsBadTime(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	_, err := FinanceSummary(mcpExternalContext(), financeResolver(), service, FinanceSummaryInput{Space: "Family", Period: "custom", From: "nope"})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceSummary(mcpExternalContext(), financeResolver(), service, FinanceSummaryInput{Space: "Family", Period: "custom", To: "nope"})
	assertMCPCode(t, err, "invalid_input")
	if service.calls != 0 {
		t.Fatal("invalid input reached the finance service")
	}
}

func TestFinanceBalancesMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{balances: &dto.BalancesReport{
		Accounts: []domainfinance.AccountBalance{{AccountID: "acc-1", Name: "BCA", Type: "bank", Balance: 1500000}, {AccountID: "acc-2", Name: "Visa", Type: "credit_card", Balance: -200000}},
		Total:    1300000,
	}}
	got, err := FinanceBalances(mcpExternalContext(), financeResolver(), service, FinanceBalancesInput{Space: "Family"})
	if err != nil {
		t.Fatalf("finance_balances: %v", err)
	}
	assertFinanceActor(t, service)
	if service.balancesSpace != financeTestSpaceID {
		t.Fatalf("space = %s", service.balancesSpace)
	}
	encoded := mustJSON(t, got)
	if !strings.Contains(encoded, `"balance":-200000`) || !strings.Contains(encoded, `"total":1300000`) || !strings.Contains(encoded, `"name":"BCA"`) {
		t.Fatalf("output = %s", encoded)
	}
}

func TestFinanceBalancesEmptyEncodesAsArray(t *testing.T) {
	service := &mcpFinanceServiceStub{balances: &dto.BalancesReport{}}
	got, err := FinanceBalances(mcpExternalContext(), financeResolver(), service, FinanceBalancesInput{Space: "Family"})
	if err != nil || mustJSON(t, got) != `{"accounts":[],"total":0}` {
		t.Fatalf("output = %s, %v", mustJSON(t, got), err)
	}
}

func TestFinanceReportDescriptionsGuideTheBot(t *testing.T) {
	d := financeDescriptions(t)
	assertDescriptionHas(t, d, "finance_summary", "today", "week", "month", "last_month", "custom", "366 days", "never compute", "WIB")
	assertDescriptionHas(t, d, "finance_balances", "never compute", "credit card")
}

func TestFinanceNewToolsMapServiceErrors(t *testing.T) {
	ctx := mcpExternalContext()
	id := "00000000-0000-0000-0000-000000000042"
	space := "Family"
	calls := map[string]func(*mcpFinanceServiceStub) error{
		"transaction_create": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceTransactionCreate(ctx, financeResolver(), s, FinanceTransactionCreateInput{Space: space})
			return err
		},
		"transaction_list": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceTransactionList(ctx, financeResolver(), s, FinanceTransactionListInput{Space: space})
			return err
		},
		"transaction_update": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceTransactionUpdate(ctx, financeResolver(), s, FinanceTransactionUpdateInput{Space: space, ID: id})
			return err
		},
		"transaction_delete": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceTransactionDelete(ctx, financeResolver(), s, FinanceTransactionDeleteInput{Space: space, ID: id})
			return err
		},
		"budget_set": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceBudgetSet(ctx, financeResolver(), s, FinanceBudgetSetInput{Space: space})
			return err
		},
		"budget_list": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceBudgetList(ctx, financeResolver(), s, FinanceBudgetListInput{Space: space})
			return err
		},
		"budget_delete": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceBudgetDelete(ctx, financeResolver(), s, FinanceBudgetDeleteInput{Space: space})
			return err
		},
		"recurring_create": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceRecurringCreate(ctx, financeResolver(), s, FinanceRecurringCreateInput{Space: space, StartAt: "2026-11-10T07:30:00+07:00"})
			return err
		},
		"recurring_list": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceRecurringList(ctx, financeResolver(), s, FinanceRecurringListInput{Space: space})
			return err
		},
		"recurring_update": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceRecurringUpdate(ctx, financeResolver(), s, FinanceRecurringUpdateInput{Space: space, ID: id})
			return err
		},
		"recurring_delete": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceRecurringDelete(ctx, financeResolver(), s, FinanceRecurringDeleteInput{Space: space, ID: id})
			return err
		},
		"summary": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceSummary(ctx, financeResolver(), s, FinanceSummaryInput{Space: space})
			return err
		},
		"balances": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceBalances(ctx, financeResolver(), s, FinanceBalancesInput{Space: space})
			return err
		},
	}
	errs := map[string]struct {
		err  error
		code string
	}{
		"forbidden":  {serviceauthorization.ErrForbidden, "forbidden"},
		"not found":  {serviceauthorization.ErrNotFound, "not_found"},
		"validation": {&serviceauthorization.ValidationError{Field: "amount", Reason: "must be positive"}, "invalid_input"},
	}
	for tool, call := range calls {
		for name, tc := range errs {
			t.Run(tool+"/"+name, func(t *testing.T) {
				assertMCPCode(t, call(&mcpFinanceServiceStub{err: tc.err}), tc.code)
			})
		}
	}
}
