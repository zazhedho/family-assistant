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
}
