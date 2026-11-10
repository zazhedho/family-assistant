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

type FinanceRecurringCreateInput struct {
	Space      string     `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Type       string     `json:"type" jsonschema:"one of income, expense, transfer"`
	Amount     MoneyInput `json:"amount" jsonschema:"amount per occurrence in rupiah, positive, with up to two decimals, e.g. 500000, 25000 or 1005422.44, as a number or a string ('.' decimal separator, no thousands separators)"`
	Account    string     `json:"account" jsonschema:"account UUID or exact name, from finance_account_list"`
	ToAccount  string     `json:"to_account,omitempty" jsonschema:"destination account UUID or exact name; required for transfer, must differ from account; blank otherwise"`
	Category   string     `json:"category,omitempty" jsonschema:"category UUID or exact name, from finance_category_list; kind must match type; blank for transfer"`
	Merchant   string     `json:"merchant,omitempty" jsonschema:"optional shop or person, e.g. Kos Bu Ani"`
	Note       string     `json:"note,omitempty" jsonschema:"optional free-text note"`
	Frequency  string     `json:"frequency" jsonschema:"one of daily, weekly, monthly"`
	IntervalN  int        `json:"interval_n,omitempty" jsonschema:"repeat every N days, weeks or months; default 1; 1 to 365 for daily and weekly, 1 to 12 for monthly"`
	DayOfMonth *int       `json:"day_of_month,omitempty" jsonschema:"monthly only: day 1-31 (clamped to short months); omit to use the WIB day of start_at"`
	StartAt    string     `json:"start_at" jsonschema:"first occurrence, RFC3339 with offset, preferably WIB, e.g. 2026-11-10T07:30:00+07:00; at most 24 hours in the past"`
	EndAt      string     `json:"end_at,omitempty" jsonschema:"optional last possible occurrence, RFC3339 with offset; omit for no end"`
}

type FinanceRecurringListInput struct {
	Space string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Limit int    `json:"limit,omitempty" jsonschema:"optional maximum results, default 50, capped at 100"`
}

type FinanceRecurringUpdateInput struct {
	Space      string      `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	ID         string      `json:"id" jsonschema:"recurring template UUID, from finance_recurring_list"`
	Type       *string     `json:"type,omitempty" jsonschema:"optional new type: income, expense or transfer"`
	Amount     *MoneyInput `json:"amount,omitempty" jsonschema:"optional new amount in rupiah, positive, with up to two decimals, e.g. 500000, 25000 or 1005422.44, as a number or a string; omit or null to leave unchanged"`
	Account    *string     `json:"account,omitempty" jsonschema:"optional new account UUID or exact name"`
	ToAccount  *string     `json:"to_account,omitempty" jsonschema:"optional new destination account UUID or exact name; empty string clears it"`
	Category   *string     `json:"category,omitempty" jsonschema:"optional new category UUID or exact name; empty string clears it; absent leaves it unchanged"`
	Merchant   *string     `json:"merchant,omitempty" jsonschema:"optional new merchant"`
	Note       *string     `json:"note,omitempty" jsonschema:"optional new note"`
	Frequency  *string     `json:"frequency,omitempty" jsonschema:"optional new frequency: daily, weekly or monthly"`
	IntervalN  *int        `json:"interval_n,omitempty" jsonschema:"optional new interval; 1 to 365 for daily and weekly, 1 to 12 for monthly"`
	DayOfMonth *int        `json:"day_of_month,omitempty" jsonschema:"optional new day 1-31; monthly only"`
	StartAt    *string     `json:"start_at,omitempty" jsonschema:"optional new first occurrence, RFC3339 with offset; omit to leave unchanged; an empty string is rejected"`
	EndAt      *string     `json:"end_at,omitempty" jsonschema:"optional new end, RFC3339 with offset; omit to leave unchanged (use clear_end_at to remove it); an empty string is rejected"`
	ClearEndAt bool        `json:"clear_end_at,omitempty" jsonschema:"true removes the end date"`
	Paused     *bool       `json:"paused,omitempty" jsonschema:"true pauses the template; false resumes it (missed occurrences while paused are skipped, not posted); omit to leave unchanged"`
}

type FinanceRecurringDeleteInput struct {
	Space string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	ID    string `json:"id" jsonschema:"recurring template UUID, from finance_recurring_list"`
}

// FinanceRecurringOutput is a template with start_at, next_run_at and end_at
// rendered in WIB.
type FinanceRecurringOutput struct {
	domainfinance.Recurring
	StartAt   time.Time  `json:"start_at"`
	NextRunAt time.Time  `json:"next_run_at"`
	EndAt     *time.Time `json:"end_at,omitempty"`
}

type FinanceRecurringListOutput struct {
	Recurring []FinanceRecurringOutput `json:"recurring"`
}

func financeRecurringOutput(r domainfinance.Recurring) FinanceRecurringOutput {
	return FinanceRecurringOutput{Recurring: r, StartAt: inWIB(r.StartAt), NextRunAt: inWIB(r.NextRunAt), EndAt: inWIBPtr(r.EndAt)}
}

func FinanceRecurringCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceRecurringCreateInput) (*FinanceRecurringOutput, error) {
	startAt, err := financeTimeRequired(input.StartAt, "start_at")
	if err != nil {
		return nil, err
	}
	endAt, err := financeTimePtr(input.EndAt, "end_at")
	if err != nil {
		return nil, err
	}
	amount, err := financeAmount(input.Amount, "amount")
	if err != nil {
		return nil, err
	}
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*FinanceRecurringOutput, error) {
		recurring, err := service.RecurringCreate(ctx, actor, dto.RecurringCreateInput{
			Space: spaceID, Type: input.Type, Amount: amount, Account: input.Account, ToAccount: input.ToAccount, Category: input.Category,
			Merchant: input.Merchant, Note: input.Note, Frequency: input.Frequency, IntervalN: input.IntervalN, DayOfMonth: input.DayOfMonth,
			StartAt: startAt, EndAt: endAt,
		})
		if err != nil {
			return nil, err
		}
		out := financeRecurringOutput(*recurring)
		return &out, nil
	})
}

func FinanceRecurringList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceRecurringListInput) (FinanceRecurringListOutput, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (FinanceRecurringListOutput, error) {
		recurring, err := service.RecurringList(ctx, actor, dto.RecurringListInput{Space: spaceID, Limit: input.Limit})
		return FinanceRecurringListOutput{Recurring: mapSlice(recurring, financeRecurringOutput)}, err
	})
}

func FinanceRecurringUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceRecurringUpdateInput) (*FinanceRecurringOutput, error) {
	id, err := financeID(input.ID, "id")
	if err != nil {
		return nil, err
	}
	startAt, err := financeTimeSet(input.StartAt, "start_at")
	if err != nil {
		return nil, err
	}
	endAt, err := financeTimeSet(input.EndAt, "end_at")
	if err != nil {
		return nil, err
	}
	amount, err := financeAmountPtr(input.Amount, "amount")
	if err != nil {
		return nil, err
	}
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*FinanceRecurringOutput, error) {
		recurring, err := service.RecurringUpdate(ctx, actor, dto.RecurringUpdateInput{
			Space: spaceID, ID: id, Type: input.Type, Amount: amount, Account: input.Account, ToAccount: input.ToAccount, Category: input.Category,
			Merchant: input.Merchant, Note: input.Note, Frequency: input.Frequency, IntervalN: input.IntervalN, DayOfMonth: input.DayOfMonth,
			StartAt: startAt, EndAt: endAt, ClearEndAt: input.ClearEndAt, Paused: input.Paused,
		})
		if err != nil {
			return nil, err
		}
		out := financeRecurringOutput(*recurring)
		return &out, nil
	})
}

func FinanceRecurringDelete(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceRecurringDeleteInput) (FinanceDeleteOutput, error) {
	id, err := financeID(input.ID, "id")
	if err != nil {
		return FinanceDeleteOutput{}, err
	}
	return financeDelete(ctx, resolver, service, input.Space, id, func(actor domainidentity.ActorContext, spaceID string) error {
		return service.RecurringDelete(ctx, actor, dto.RecurringDeleteInput{Space: spaceID, ID: id})
	})
}

func registerFinanceRecurringTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	addFinanceTool(server, &mcpsdk.Tool{
		Name: "finance_recurring_create",
		Description: "Create a recurring transaction template (rent, salary, subscription) that posts automatically. frequency is daily, weekly or monthly, every interval_n (default 1). start_at is the first occurrence and may be at most 24 hours in the past; the first run is the first matching date on or after start_at. day_of_month is only for monthly (omit it to use the day of start_at in WIB; 29-31 are clamped in short months). end_at is optional." + financeKindRules +
			financeTimeRules + financeConfirmRules + financeAdminOnly + financeRules,
	}, func(ctx context.Context, input FinanceRecurringCreateInput) (*FinanceRecurringOutput, error) {
		return FinanceRecurringCreate(ctx, resolver, service, input)
	})
	addFinanceTool(server, &mcpsdk.Tool{
		Name: "finance_recurring_list",
		Description: "List the recurring transaction templates of a finance Space with next_run_at, paused state and end_at (times in WIB). A paused template with a pause_reason needs attention: tell the admin what the reason says (it was paused automatically after failing). Any member can list." +
			financeRules,
	}, func(ctx context.Context, input FinanceRecurringListInput) (FinanceRecurringListOutput, error) {
		return FinanceRecurringList(ctx, resolver, service, input)
	})
	addFinanceTool(server, &mcpsdk.Tool{
		Name: "finance_recurring_update",
		Description: "Change a recurring template by id (UUID, from finance_recurring_list). Only the fields you send change. An empty string for category or to_account clears it; clear_end_at=true removes the end date. paused=true pauses it and paused=false resumes it; unpausing skips missed occurrences (they are not posted retroactively). Switching to daily or weekly drops day_of_month." + financeKindRules +
			financeTimeRules + financeConfirmRules + financeAdminOnly + financeRules,
	}, func(ctx context.Context, input FinanceRecurringUpdateInput) (*FinanceRecurringOutput, error) {
		return FinanceRecurringUpdate(ctx, resolver, service, input)
	})
	addFinanceTool(server, &mcpsdk.Tool{
		Name: "finance_recurring_delete",
		Description: "Delete a recurring template by id (UUID, from finance_recurring_list). Transactions it already posted stay. Confirm with the user before deleting." +
			financeAdminOnly + financeRules,
	}, func(ctx context.Context, input FinanceRecurringDeleteInput) (FinanceDeleteOutput, error) {
		return FinanceRecurringDelete(ctx, resolver, service, input)
	})
}
