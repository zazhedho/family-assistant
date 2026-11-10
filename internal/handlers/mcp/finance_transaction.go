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

type FinanceTransactionCreateInput struct {
	Space      string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Type       string `json:"type" jsonschema:"one of income, expense, transfer"`
	Amount     int64  `json:"amount" jsonschema:"amount as a positive integer in whole rupiah, e.g. 25000 for Rp 25.000; never a string or a decimal"`
	Account    string `json:"account" jsonschema:"account UUID or exact name, from finance_account_list; the source account (the destination for income)"`
	ToAccount  string `json:"to_account,omitempty" jsonschema:"destination account UUID or exact name; required for transfer, must differ from account; leave blank otherwise"`
	Category   string `json:"category,omitempty" jsonschema:"category UUID or exact name, from finance_category_list; kind must match type (income or expense); leave blank for transfer"`
	Merchant   string `json:"merchant,omitempty" jsonschema:"optional shop or person, e.g. Indomaret"`
	Note       string `json:"note,omitempty" jsonschema:"optional free-text note"`
	OccurredAt string `json:"occurred_at,omitempty" jsonschema:"RFC3339 time WITH offset, preferably WIB, e.g. 2026-11-10T07:30:00+07:00; blank means now"`
}

type FinanceTransactionListInput struct {
	Space    string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	Type     string `json:"type,omitempty" jsonschema:"optional filter: income, expense or transfer"`
	Category string `json:"category,omitempty" jsonschema:"optional filter: category UUID or exact name"`
	Account  string `json:"account,omitempty" jsonschema:"optional filter: account UUID or exact name (matches source or destination)"`
	Search   string `json:"search,omitempty" jsonschema:"optional text matched against merchant and note"`
	From     string `json:"from,omitempty" jsonschema:"optional inclusive lower bound, RFC3339 with offset, e.g. 2026-11-01T00:00:00+07:00"`
	To       string `json:"to,omitempty" jsonschema:"optional exclusive upper bound, RFC3339 with offset, e.g. 2026-12-01T00:00:00+07:00"`
	Limit    int    `json:"limit,omitempty" jsonschema:"optional maximum results, default 50, capped at 100"`
}

type FinanceTransactionUpdateInput struct {
	Space      string  `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	ID         string  `json:"id" jsonschema:"transaction UUID, from finance_transaction_list"`
	Type       *string `json:"type,omitempty" jsonschema:"optional new type: income, expense or transfer"`
	Amount     *int64  `json:"amount,omitempty" jsonschema:"optional new amount, positive integer in whole rupiah, e.g. 25000"`
	Account    *string `json:"account,omitempty" jsonschema:"optional new account UUID or exact name"`
	ToAccount  *string `json:"to_account,omitempty" jsonschema:"optional new destination account UUID or exact name; empty string clears it"`
	Category   *string `json:"category,omitempty" jsonschema:"optional new category UUID or exact name; an empty string clears the category; absent leaves it unchanged"`
	Merchant   *string `json:"merchant,omitempty" jsonschema:"optional new merchant; empty string clears it"`
	Note       *string `json:"note,omitempty" jsonschema:"optional new note; empty string clears it"`
	OccurredAt *string `json:"occurred_at,omitempty" jsonschema:"optional new time, RFC3339 with offset, e.g. 2026-11-10T07:30:00+07:00; omit to leave unchanged; an empty string is rejected"`
}

type FinanceTransactionDeleteInput struct {
	Space string `json:"space,omitempty" jsonschema:"finance Space UUID or exact user-facing name. In a private chat always name the finance Space: a blank value selects the Personal Space, which is never a finance Space (not found). In a group or other shared chat blank selects the bound Space"`
	ID    string `json:"id" jsonschema:"transaction UUID, from finance_transaction_list or the create result"`
}

// FinanceTransactionOutput is a transaction with occurred_at rendered in WIB.
type FinanceTransactionOutput struct {
	domainfinance.Transaction
	OccurredAt time.Time `json:"occurred_at"`
}

// FinanceTransactionResultOutput is the create/update result: the transaction
// plus the warnings the bot must relay.
type FinanceTransactionResultOutput struct {
	FinanceTransactionOutput
	PossibleDuplicate *FinanceTransactionOutput   `json:"possible_duplicate,omitempty"`
	BudgetStatus      *domainfinance.BudgetStatus `json:"budget_status,omitempty"`
}

type FinanceTransactionListOutput struct {
	Transactions []FinanceTransactionOutput `json:"transactions"`
}

func financeTransactionOutput(tx domainfinance.Transaction) FinanceTransactionOutput {
	return FinanceTransactionOutput{Transaction: tx, OccurredAt: inWIB(tx.OccurredAt)}
}

func financeTransactionResult(result *dto.TransactionResult) *FinanceTransactionResultOutput {
	out := &FinanceTransactionResultOutput{FinanceTransactionOutput: financeTransactionOutput(result.Transaction), BudgetStatus: result.BudgetStatus}
	if result.PossibleDuplicate != nil {
		duplicate := financeTransactionOutput(*result.PossibleDuplicate)
		out.PossibleDuplicate = &duplicate
	}
	return out
}

func FinanceTransactionCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceTransactionCreateInput) (*FinanceTransactionResultOutput, error) {
	occurredAt, err := financeTime(input.OccurredAt, "occurred_at")
	if err != nil {
		return nil, err
	}
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*FinanceTransactionResultOutput, error) {
		result, err := service.TransactionCreate(ctx, actor, dto.TransactionCreateInput{
			Space: spaceID, Type: input.Type, Amount: input.Amount, Account: input.Account, ToAccount: input.ToAccount, Category: input.Category,
			Merchant: input.Merchant, Note: input.Note, OccurredAt: occurredAt,
		})
		if err != nil {
			return nil, err
		}
		return financeTransactionResult(result), nil
	})
}

func FinanceTransactionList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceTransactionListInput) (FinanceTransactionListOutput, error) {
	from, err := financeTimePtr(input.From, "from")
	if err != nil {
		return FinanceTransactionListOutput{}, err
	}
	to, err := financeTimePtr(input.To, "to")
	if err != nil {
		return FinanceTransactionListOutput{}, err
	}
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (FinanceTransactionListOutput, error) {
		transactions, err := service.TransactionList(ctx, actor, dto.TransactionListInput{
			Space: spaceID, Type: input.Type, Category: input.Category, Account: input.Account, Search: input.Search, From: from, To: to, Limit: input.Limit,
		})
		return FinanceTransactionListOutput{Transactions: mapSlice(transactions, financeTransactionOutput)}, err
	})
}

func FinanceTransactionUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceTransactionUpdateInput) (*FinanceTransactionResultOutput, error) {
	id, err := financeID(input.ID, "id")
	if err != nil {
		return nil, err
	}
	occurredAt, err := financeTimeSet(input.OccurredAt, "occurred_at")
	if err != nil {
		return nil, err
	}
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*FinanceTransactionResultOutput, error) {
		result, err := service.TransactionUpdate(ctx, actor, spaceID, id, dto.TransactionUpdateInput{
			Type: input.Type, Amount: input.Amount, Account: input.Account, ToAccount: input.ToAccount, Category: input.Category,
			Merchant: input.Merchant, Note: input.Note, OccurredAt: occurredAt,
		})
		if err != nil {
			return nil, err
		}
		return financeTransactionResult(result), nil
	})
}

func FinanceTransactionDelete(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceTransactionDeleteInput) (FinanceDeleteOutput, error) {
	id, err := financeID(input.ID, "id")
	if err != nil {
		return FinanceDeleteOutput{}, err
	}
	return financeDelete(ctx, resolver, service, input.Space, id, func(actor domainidentity.ActorContext, spaceID string) error {
		return service.TransactionDelete(ctx, actor, spaceID, id)
	})
}

func registerFinanceTransactionTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "finance_transaction_create",
		Description: "Record an income, expense or transfer in a finance Space (the family's books). amount is whole rupiah (25000 = Rp 25.000)." + financeKindRules +
			" account and category are UUIDs or exact names from finance_account_list and finance_category_list. A blank occurred_at means now; otherwise pass an RFC3339 time with offset, preferably WIB." + financeTimeRules +
			financeConfirmRules +
			" The result is the saved transaction (its id, amount, occurred_at in WIB) plus two optional fields. If possible_duplicate is present, the transaction is ALREADY SAVED: tell the user a similar one exists, ask whether it was a double entry, and if so offer to delete the new one with finance_transaction_delete (owner/admin only; otherwise ask an admin). If budget_status is present and its percent_used is 80 or more, mention it (budget, spent, remaining)." +
			financeNoMathRules + financeTxPermRules + financeRules,
	}, func(ctx context.Context, input FinanceTransactionCreateInput) (*FinanceTransactionResultOutput, error) {
		return FinanceTransactionCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "finance_transaction_list",
		Description: "List transactions of a finance Space, newest first, optionally filtered by type, category, account, search text and a time range (from inclusive, to exclusive, RFC3339 with offset, preferably WIB). Use it to find a transaction id before updating or deleting. Category and account are UUIDs or exact names from finance_category_list and finance_account_list. Times in the result are WIB. For totals use finance_summary instead of adding rows up." +
			financeNoMathRules + financeTxPermRules + financeRules,
	}, func(ctx context.Context, input FinanceTransactionListInput) (FinanceTransactionListOutput, error) {
		return FinanceTransactionList(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "finance_transaction_update",
		Description: "Change a transaction by id (UUID, from finance_transaction_list or the create result). Only the fields you send change; an absent field is left unchanged. An empty string for category clears the category, and an empty string for to_account clears the destination. The same rules as create apply to the resulting transaction." + financeKindRules +
			financeTimeRules + financeConfirmRules + " The result is the updated transaction plus budget_status when the category has a budget (relay it as for create: mention it at 80 percent or more); update does not return possible_duplicate." +
			financeNoMathRules + financeTxPermRules + financeRules,
	}, func(ctx context.Context, input FinanceTransactionUpdateInput) (*FinanceTransactionResultOutput, error) {
		return FinanceTransactionUpdate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "finance_transaction_delete",
		Description: "Delete a transaction by id (UUID, from finance_transaction_list or the create result); balances and reports stop counting it. Confirm with the user before deleting." +
			financeAdminOnly + financeRules,
	}, func(ctx context.Context, input FinanceTransactionDeleteInput) (FinanceDeleteOutput, error) {
		return FinanceTransactionDelete(ctx, resolver, service, input)
	})
}
