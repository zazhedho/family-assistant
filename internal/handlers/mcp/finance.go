package mcp

import (
	"context"
	"errors"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	interfaceidentity "family-assistant/internal/interfaces/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// financeRules is appended to the description of every finance tool: it is the
// bot's only manual for the rules the backend enforces.
const financeRules = " A finance Space is required (a Space whose category is finance); other Spaces are rejected. Amounts are integers in whole rupiah (Rp 1.500.000 is 1500000), never decimals. Never invent ids: use the list tool first, or pass the exact name."

// financeAdminRules is added to tools that change accounts or categories.
const financeAdminRules = " Members can list but only an owner or admin can create or change accounts and categories; if the result is forbidden, tell the user to ask an admin. Names are unique per Space (case-insensitive). There is no delete: archive instead (archived=true), which keeps past transactions and reports intact."

// financeCall resolves the actor and the selected Space and runs call with
// them; every finance tool goes through it, so nil-service handling, Space
// selection and error mapping stay identical.
func financeCall[Out any](ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, space string,
	call func(actor domainidentity.ActorContext, spaceID string) (Out, error),
) (Out, error) {
	var zero Out
	actor, err := selectSpaceActor(ctx, resolver, space)
	if err != nil {
		return zero, MapToolError(err)
	}
	if service == nil {
		return zero, MapToolError(errors.New("finance service is not configured"))
	}
	out, err := call(actor, actor.SpaceID)
	if err != nil {
		return zero, MapToolError(err)
	}
	return out, nil
}

// nonNilSlice makes a nil slice encode as [] rather than null.
func nonNilSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func registerFinanceTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	registerFinanceAccountTools(server, resolver, service)
	registerFinanceCategoryTools(server, resolver, service)
}

type FinanceAccountCreateInput struct {
	Space          string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	Name           string `json:"name" jsonschema:"account name, unique per Space (case-insensitive), e.g. BCA, Cash, GoPay"`
	Type           string `json:"type" jsonschema:"one of cash, bank, ewallet, savings, credit_card"`
	OpeningBalance int64  `json:"opening_balance,omitempty" jsonschema:"starting balance as an integer in whole rupiah, e.g. 1500000 for Rp 1.500.000; omit or 0 for none. May be negative only for credit_card"`
}

type FinanceAccountListInput struct {
	Space           string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"also list archived accounts; default false"`
	Limit           int    `json:"limit,omitempty" jsonschema:"optional maximum results, default 50, capped at 100"`
}

type FinanceAccountUpdateInput struct {
	Space    string  `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	Account  string  `json:"account" jsonschema:"account UUID or exact name, from finance_account_list"`
	Name     *string `json:"name,omitempty" jsonschema:"optional new name, unique per Space (case-insensitive)"`
	Archived *bool   `json:"archived,omitempty" jsonschema:"true archives the account (hides it, keeps its transactions); false unarchives it; omit to leave unchanged"`
}

type FinanceAccountListOutput struct {
	Accounts []domainfinance.Account `json:"accounts"`
}

func FinanceAccountCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceAccountCreateInput) (*domainfinance.Account, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*domainfinance.Account, error) {
		return service.AccountCreate(ctx, actor, dto.AccountCreateInput{Space: spaceID, Name: input.Name, Type: input.Type, OpeningBalance: input.OpeningBalance})
	})
}

func FinanceAccountList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceAccountListInput) ([]domainfinance.Account, error) {
	accounts, err := financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) ([]domainfinance.Account, error) {
		return service.AccountList(ctx, actor, dto.AccountListInput{Space: spaceID, IncludeArchived: input.IncludeArchived, Limit: input.Limit})
	})
	return nonNilSlice(accounts), err
}

func FinanceAccountUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceAccountUpdateInput) (*domainfinance.Account, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*domainfinance.Account, error) {
		return service.AccountUpdate(ctx, actor, dto.AccountUpdateInput{Space: spaceID, ID: input.Account, Name: input.Name, Archived: input.Archived})
	})
}

func registerFinanceAccountTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	addTool(server, &mcpsdk.Tool{
		Name:        "finance_account_create",
		Description: "Create a money account (cash, bank, e-wallet, savings or credit card) in a finance Space. opening_balance is the starting balance in whole rupiah." + financeAdminRules + financeRules + " Check finance_account_list first to avoid duplicates.",
	}, func(ctx context.Context, input FinanceAccountCreateInput) (*domainfinance.Account, error) {
		return FinanceAccountCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name:        "finance_account_list",
		Description: "List the money accounts of a finance Space (id, name, type, opening_balance; archived ones only with include_archived). This is the list tool to use for account ids and names; it does not compute current balances. Any member can list." + financeRules,
	}, func(ctx context.Context, input FinanceAccountListInput) (FinanceAccountListOutput, error) {
		accounts, err := FinanceAccountList(ctx, resolver, service, input)
		return FinanceAccountListOutput{Accounts: accounts}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name:        "finance_account_update",
		Description: "Rename or archive/unarchive a money account in a finance Space. The account is its UUID or exact name; get it from finance_account_list. Type and opening balance cannot be changed here." + financeAdminRules + financeRules,
	}, func(ctx context.Context, input FinanceAccountUpdateInput) (*domainfinance.Account, error) {
		return FinanceAccountUpdate(ctx, resolver, service, input)
	})
}
