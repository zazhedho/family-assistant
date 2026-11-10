package interfacefinance

import (
	"context"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
)

type ServiceFinanceInterface interface {
	AccountCreate(context.Context, domainidentity.ActorContext, dto.AccountCreateInput) (*domainfinance.Account, error)
	AccountList(context.Context, domainidentity.ActorContext, dto.AccountListInput) ([]domainfinance.Account, error)
	AccountUpdate(context.Context, domainidentity.ActorContext, dto.AccountUpdateInput) (*domainfinance.Account, error)
	CategoryCreate(context.Context, domainidentity.ActorContext, dto.CategoryCreateInput) (*domainfinance.Category, error)
	CategoryList(context.Context, domainidentity.ActorContext, dto.CategoryListInput) ([]domainfinance.Category, error)
	CategoryUpdate(context.Context, domainidentity.ActorContext, dto.CategoryUpdateInput) (*domainfinance.Category, error)
	TransactionCreate(context.Context, domainidentity.ActorContext, dto.TransactionCreateInput) (*dto.TransactionResult, error)
	TransactionList(context.Context, domainidentity.ActorContext, dto.TransactionListInput) ([]domainfinance.Transaction, error)
	TransactionUpdate(ctx context.Context, actor domainidentity.ActorContext, spaceID, id string, input dto.TransactionUpdateInput) (*dto.TransactionResult, error)
	TransactionDelete(ctx context.Context, actor domainidentity.ActorContext, spaceID, id string) error
	BudgetSet(context.Context, domainidentity.ActorContext, dto.BudgetSetInput) (*domainfinance.Budget, error)
	// BudgetList reports every budget of month ("YYYY-MM", "" = current WIB month).
	BudgetList(ctx context.Context, actor domainidentity.ActorContext, spaceID, month string) ([]dto.BudgetStatusView, error)
	BudgetDelete(context.Context, domainidentity.ActorContext, dto.BudgetDeleteInput) error
	Balances(ctx context.Context, actor domainidentity.ActorContext, spaceID string) (*dto.BalancesReport, error)
	Summary(context.Context, domainidentity.ActorContext, dto.SummaryInput) (*dto.SummaryReport, error)
	RecurringCreate(context.Context, domainidentity.ActorContext, dto.RecurringCreateInput) (*domainfinance.Recurring, error)
	RecurringList(context.Context, domainidentity.ActorContext, dto.RecurringListInput) ([]domainfinance.Recurring, error)
	RecurringUpdate(context.Context, domainidentity.ActorContext, dto.RecurringUpdateInput) (*domainfinance.Recurring, error)
	RecurringDelete(context.Context, domainidentity.ActorContext, dto.RecurringDeleteInput) error
}
