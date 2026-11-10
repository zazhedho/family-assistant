package mcp

import (
	"context"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	interfaceidentity "family-assistant/internal/interfaces/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type FinanceSummaryInput struct {
	Space  string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Period string `json:"period,omitempty" jsonschema:"one of today, week (Monday start), month, last_month, custom; blank means month"`
	From   string `json:"from,omitempty" jsonschema:"custom only (blank period then implies custom): inclusive start, RFC3339 with offset, e.g. 2026-11-01T00:00:00+07:00"`
	To     string `json:"to,omitempty" jsonschema:"custom only (blank period then implies custom): exclusive end, RFC3339 with offset, e.g. 2026-12-01T00:00:00+07:00; at most 366 days after from"`
}

type FinanceBalancesInput struct {
	Space string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
}

// FinanceSummaryOutput is a summary with from/to rendered in WIB.
type FinanceSummaryOutput struct {
	Period          string                        `json:"period"`
	From            time.Time                     `json:"from"`
	To              time.Time                     `json:"to"`
	Income          int64                         `json:"income"`
	Expense         int64                         `json:"expense"`
	Net             int64                         `json:"net"`
	PreviousIncome  int64                         `json:"previous_income"`
	PreviousExpense int64                         `json:"previous_expense"`
	ByCategory      []domainfinance.CategoryTotal `json:"by_category"`
	ByAccount       []domainfinance.AccountTotal  `json:"by_account"`
	TopMerchants    []domainfinance.MerchantTotal `json:"top_merchants"`
}

type FinanceBalancesOutput struct {
	Accounts []domainfinance.AccountBalance `json:"accounts"`
	Total    int64                          `json:"total"`
}

func FinanceSummary(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceSummaryInput) (*FinanceSummaryOutput, error) {
	from, err := financeTimePtr(input.From, "from")
	if err != nil {
		return nil, err
	}
	to, err := financeTimePtr(input.To, "to")
	if err != nil {
		return nil, err
	}
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*FinanceSummaryOutput, error) {
		report, err := service.Summary(ctx, actor, dto.SummaryInput{Space: spaceID, Period: input.Period, From: from, To: to})
		if err != nil {
			return nil, err
		}
		return &FinanceSummaryOutput{
			Period: report.Period, From: inWIB(report.From), To: inWIB(report.To),
			Income: report.Income, Expense: report.Expense, Net: report.Net, PreviousIncome: report.PreviousIncome, PreviousExpense: report.PreviousExpense,
			ByCategory: nonNilSlice(report.ByCategory), ByAccount: nonNilSlice(report.ByAccount), TopMerchants: nonNilSlice(report.TopMerchants),
		}, nil
	})
}

func FinanceBalances(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceBalancesInput) (*FinanceBalancesOutput, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*FinanceBalancesOutput, error) {
		report, err := service.Balances(ctx, actor, spaceID)
		if err != nil {
			return nil, err
		}
		return &FinanceBalancesOutput{Accounts: nonNilSlice(report.Accounts), Total: report.Total}, nil
	})
}

func registerFinanceReportTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "finance_summary",
		Description: "Income, expense and net for a period with the previous period, plus by_category and top_merchants (EXPENSE only) and by_account (income and expense per account). period is today, week (Monday start), month, last_month or custom, all in WIB; blank means month. from (inclusive) and to (exclusive), RFC3339 with offset, at most 366 days apart, apply to custom only: with a blank period they imply custom (both are then required), with any other period they are rejected. from and to in the result are WIB. Transfers count in neither income nor expense. Answer spending questions only from this tool's output." +
			financeNoMathRules + " Any member can read." + financeRules,
	}, func(ctx context.Context, input FinanceSummaryInput) (*FinanceSummaryOutput, error) {
		return FinanceSummary(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "finance_balances",
		Description: "Current balance of every non-archived money account and their total (opening balance plus all transactions). A credit card balance is negative (money owed) and is included in the total. Answer balance questions only from this tool's output." +
			financeNoMathRules + " Any member can read." + financeRules,
	}, func(ctx context.Context, input FinanceBalancesInput) (*FinanceBalancesOutput, error) {
		return FinanceBalances(ctx, resolver, service, input)
	})
}
