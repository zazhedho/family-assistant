package mcp

import (
	"context"
	"errors"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// financeRules is appended to the description of every finance tool: it is the
// bot's only manual for the rules the backend enforces.
const financeRules = " A finance Space is required (a Space whose category is finance); other Spaces are rejected. Amounts are integers in whole rupiah (Rp 1.500.000 is 1500000), never decimals. Never invent ids: use the list tool first, or pass the exact name."

// financeAdminRules is added to tools that change accounts or categories.
const financeAdminRules = " Members can list but only an owner or admin can create or change accounts and categories; if the result is forbidden, tell the user to ask an admin. Names are unique per Space (case-insensitive). There is no delete: archive via the update tool (archived=true), which keeps past transactions and reports intact."

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

// mapSlice converts items and never returns nil, so the result encodes as [].
func mapSlice[T, U any](items []T, convert func(T) U) []U {
	out := make([]U, 0, len(items))
	for _, item := range items {
		out = append(out, convert(item))
	}
	return out
}

// FinanceDeleteOutput confirms a delete; the record is soft-deleted.
type FinanceDeleteOutput struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id,omitempty"`
}

// financeParseTime parses an RFC3339 time with offset; a bad value becomes a
// field error the model can act on.
func financeParseTime(value, field string, required bool) (*time.Time, error) {
	parsed, err := utils.ParseRFC3339(value, field, required)
	if err != nil {
		reason := "must be an RFC3339 timestamp with offset, e.g. 2026-11-10T07:30:00+07:00"
		if required && value == "" {
			reason = "is required (RFC3339 with offset, e.g. 2026-11-10T07:30:00+07:00)"
		}
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: field, Reason: reason})
	}
	return parsed, nil
}

// financeTime parses an optional RFC3339 time; blank is the zero time.
func financeTime(value, field string) (time.Time, error) {
	parsed, err := financeParseTime(value, field, false)
	if err != nil || parsed == nil {
		return time.Time{}, err
	}
	return *parsed, nil
}

// financeTimePtr parses an optional RFC3339 time; blank is nil.
func financeTimePtr(value, field string) (*time.Time, error) {
	return financeParseTime(value, field, false)
}

// financeTimeSet parses a time that was explicitly provided: nil stays nil, and
// an empty string is rejected rather than treated as unset.
func financeTimeSet(value *string, field string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if *value == "" {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: field, Reason: "must not be an empty string; omit the field to leave it unchanged"})
	}
	return financeParseTime(*value, field, true)
}

// financeTimeRequired parses a mandatory RFC3339 time.
func financeTimeRequired(value, field string) (time.Time, error) {
	parsed, err := financeParseTime(value, field, true)
	if err != nil {
		return time.Time{}, err
	}
	return *parsed, nil
}

func financeID(value, field string) (string, error) {
	id, err := utils.ParseUUID(value, field, true)
	if err != nil {
		return "", MapToolError(&serviceauthorization.ValidationError{Field: field, Reason: "must be a UUID from the list tool"})
	}
	return id, nil
}

// inWIB renders a time in Asia/Jakarta, the zone the family reads.
func inWIB(t time.Time) time.Time { return t.In(domainfinance.WIB()) }

func inWIBPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	converted := inWIB(*t)
	return &converted
}

// financeDelete runs a delete through financeCall and shapes the confirmation.
func financeDelete(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, space, id string,
	call func(actor domainidentity.ActorContext, spaceID string) error,
) (FinanceDeleteOutput, error) {
	return financeCall(ctx, resolver, service, space, func(actor domainidentity.ActorContext, spaceID string) (FinanceDeleteOutput, error) {
		if err := call(actor, spaceID); err != nil {
			return FinanceDeleteOutput{}, err
		}
		return FinanceDeleteOutput{Deleted: true, ID: id}, nil
	})
}

const (
	financeConfirmRules = " Confirm with the user before writing when the amount, account or category is ambiguous or comes from a photo or voice note; do not guess."
	financeTimeRules    = " Times are RFC3339 strings WITH an offset; use WIB (+07:00), for example 2026-11-10T07:30:00+07:00."
	financeNoMathRules  = " Take every amount, total and percentage from the tool output; never compute or estimate them yourself."
	financeKindRules    = " type is income, expense or transfer. A transfer needs to_account (a different account) and no category; income and expense must not have a to_account, and a category must match the type (income category for income, expense category for expense). When changing a transfer to income or expense send to_account as an empty string to clear the destination; when changing an income or expense to a transfer send category as an empty string to clear the category."
	financeTxPermRules  = " Members can list all transactions of the Space and create new ones, and can update only the transactions they created; only an owner or admin can update anyone's transaction, delete a transaction or manage budgets and recurring templates. If the result is forbidden, tell the user to ask an admin."
	financeAdminOnly    = " Only an owner or admin can do this; if the result is forbidden, tell the user to ask an admin."
)

func registerFinanceTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	registerFinanceAccountTools(server, resolver, service)
	registerFinanceCategoryTools(server, resolver, service)
	registerFinanceTransactionTools(server, resolver, service)
	registerFinanceBudgetTools(server, resolver, service)
	registerFinanceRecurringTools(server, resolver, service)
	registerFinanceReportTools(server, resolver, service)
}

type FinanceAccountCreateInput struct {
	Space          string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Name           string `json:"name" jsonschema:"account name, unique per Space (case-insensitive), e.g. BCA, Cash, GoPay"`
	Type           string `json:"type" jsonschema:"one of cash, bank, ewallet, savings, credit_card"`
	OpeningBalance int64  `json:"opening_balance,omitempty" jsonschema:"starting balance as an integer in whole rupiah, e.g. 1500000 for Rp 1.500.000; omit or 0 for none. May be negative only for credit_card"`
}

type FinanceAccountListInput struct {
	Space           string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"also list archived accounts; default false"`
	Limit           int    `json:"limit,omitempty" jsonschema:"optional maximum results, default 50, capped at 100"`
}

type FinanceAccountUpdateInput struct {
	Space    string  `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Account  string  `json:"account" jsonschema:"account UUID or exact name, from finance_account_list"`
	Name     *string `json:"name,omitempty" jsonschema:"optional new name, unique per Space (case-insensitive)"`
	Archived *bool   `json:"archived,omitempty" jsonschema:"true archives the account (hides it, keeps its transactions); false unarchives it; omit to leave unchanged"`
}

type FinanceAccountListOutput struct {
	Accounts []domainfinance.Account `json:"accounts"`
}

func FinanceAccountCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceAccountCreateInput) (*domainfinance.Account, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*domainfinance.Account, error) {
		return service.AccountCreate(ctx, actor, dto.AccountCreateInput{Space: spaceID, Name: input.Name, Type: input.Type, OpeningBalance: wholeRupiah(input.OpeningBalance)})
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

// wholeRupiah converts a tool's whole-rupiah integer input to Money (sen).
// It is a stopgap until the tools accept decimal amounts: a value beyond the
// range is saturated to just outside MaxAmount (instead of overflowing) so the
// service validators reject it.
func wholeRupiah(v int64) domainfinance.Money {
	limit := int64(domainfinance.MaxAmount) / 100
	switch {
	case v > limit:
		return domainfinance.MaxAmount + 1
	case v < -limit:
		return -domainfinance.MaxAmount - 1
	}
	return domainfinance.Money(v) * 100
}

func wholeRupiahPtr(v *int64) *domainfinance.Money {
	if v == nil {
		return nil
	}
	m := wholeRupiah(*v)
	return &m
}
