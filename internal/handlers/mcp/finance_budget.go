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

type FinanceBudgetSetInput struct {
	Space    string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Category string `json:"category" jsonschema:"expense category UUID or exact name, from finance_category_list"`
	Month    string `json:"month" jsonschema:"calendar month in WIB as YYYY-MM, e.g. 2026-11"`
	Amount   int64  `json:"amount" jsonschema:"monthly budget as a positive integer in whole rupiah, e.g. 2000000 for Rp 2.000.000"`
}

type FinanceBudgetListInput struct {
	Space string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Month string `json:"month,omitempty" jsonschema:"optional calendar month in WIB as YYYY-MM, e.g. 2026-11; blank means the current month"`
}

type FinanceBudgetDeleteInput struct {
	Space    string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Category string `json:"category" jsonschema:"expense category UUID or exact name"`
	Month    string `json:"month" jsonschema:"calendar month in WIB as YYYY-MM, e.g. 2026-11"`
}

// FinanceBudgetOutput is a budget with its month as "YYYY-MM".
type FinanceBudgetOutput struct {
	ID         string    `json:"id"`
	SpaceID    string    `json:"space_id"`
	CategoryID string    `json:"category_id"`
	Month      string    `json:"month"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type FinanceBudgetListOutput struct {
	Budgets []dto.BudgetStatusView `json:"budgets"`
}

func FinanceBudgetSet(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceBudgetSetInput) (*FinanceBudgetOutput, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*FinanceBudgetOutput, error) {
		budget, err := service.BudgetSet(ctx, actor, dto.BudgetSetInput{Space: spaceID, Category: input.Category, Month: input.Month, Amount: input.Amount})
		if err != nil {
			return nil, err
		}
		return financeBudgetOutput(budget), nil
	})
}

func financeBudgetOutput(b *domainfinance.Budget) *FinanceBudgetOutput {
	return &FinanceBudgetOutput{
		ID: b.ID, SpaceID: b.SpaceID, CategoryID: b.CategoryID, Month: b.Month.Format("2006-01"), Amount: b.Amount,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}

func FinanceBudgetList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceBudgetListInput) (FinanceBudgetListOutput, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (FinanceBudgetListOutput, error) {
		budgets, err := service.BudgetList(ctx, actor, spaceID, input.Month)
		return FinanceBudgetListOutput{Budgets: nonNilSlice(budgets)}, err
	})
}

func FinanceBudgetDelete(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceBudgetDeleteInput) (FinanceDeleteOutput, error) {
	return financeDelete(ctx, resolver, service, input.Space, "", func(actor domainidentity.ActorContext, spaceID string) error {
		return service.BudgetDelete(ctx, actor, dto.BudgetDeleteInput{Space: spaceID, Category: input.Category, Month: input.Month})
	})
}

func registerFinanceBudgetTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "finance_budget_set",
		Description: "Set (or replace) the monthly budget of one expense category for a month. month is YYYY-MM (WIB calendar month), within +-24 months of the current month. Budgets exist only for expense categories, not income. amount is whole rupiah (2000000 = Rp 2.000.000). category is a UUID or exact name from finance_category_list." +
			financeAdminOnly + financeRules,
	}, func(ctx context.Context, input FinanceBudgetSetInput) (*FinanceBudgetOutput, error) {
		return FinanceBudgetSet(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "finance_budget_list",
		Description: "List every budget of a month with its status: category_name, budget, spent, remaining and percent_used. month is YYYY-MM (WIB) within 24 months of the current month; blank means the current month. Any member can list. If percent_used is 80 or more, mention it." +
			financeNoMathRules + financeRules,
	}, func(ctx context.Context, input FinanceBudgetListInput) (FinanceBudgetListOutput, error) {
		return FinanceBudgetList(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "finance_budget_delete",
		Description: "Remove the budget of an expense category for a month (month is YYYY-MM, WIB, within 24 months of the current month). Past transactions are not affected. Confirm with the user before deleting." +
			financeAdminOnly + financeRules,
	}, func(ctx context.Context, input FinanceBudgetDeleteInput) (FinanceDeleteOutput, error) {
		return FinanceBudgetDelete(ctx, resolver, service, input)
	})
}
