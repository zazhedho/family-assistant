package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
)

var financeTestOccurredAt = time.Date(2026, 11, 10, 0, 30, 0, 0, time.UTC) // 07:30 WIB

func financeTestTransaction() domainfinance.Transaction {
	category := "cat-1"
	return domainfinance.Transaction{
		ID: "tx-1", SpaceID: financeTestSpaceID, Type: "expense", Amount: 25000, AccountID: "acc-1", CategoryID: &category,
		Merchant: "Warung", Note: "lunch", OccurredAt: financeTestOccurredAt,
		CreatedAt: financeTestOccurredAt, UpdatedAt: financeTestOccurredAt,
	}
}

func encodeToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(mustJSON(t, v)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func financeDescriptions(t *testing.T) map[string]string {
	t.Helper()
	descriptions := map[string]string{}
	for _, tool := range financeTools(t, ToolServices{Finance: &mcpFinanceServiceStub{}}) {
		descriptions[tool.Name] = tool.Description
	}
	return descriptions
}

func assertDescriptionHas(t *testing.T, descriptions map[string]string, tool string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(strings.ToLower(descriptions[tool]), strings.ToLower(want)) {
			t.Errorf("%s description lacks %q", tool, want)
		}
	}
}

func TestFinanceTransactionCreateMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{txResult: &dto.TransactionResult{Transaction: financeTestTransaction()}}
	_, err := FinanceTransactionCreate(mcpExternalContext(), financeResolver(), service, FinanceTransactionCreateInput{
		Space: "Family", Type: "transfer", Amount: 25000, Account: "BCA", ToAccount: "Cash", Category: "Makan",
		Merchant: "Warung", Note: "lunch", OccurredAt: "2026-11-10T07:30:00+07:00",
	})
	if err != nil {
		t.Fatalf("finance_transaction_create: %v", err)
	}
	assertFinanceActor(t, service)
	in := service.txCreate
	if in.Space != financeTestSpaceID || in.Type != "transfer" || in.Amount != 25000 || in.Account != "BCA" || in.ToAccount != "Cash" ||
		in.Category != "Makan" || in.Merchant != "Warung" || in.Note != "lunch" || !in.OccurredAt.Equal(financeTestOccurredAt) {
		t.Fatalf("input = %+v", in)
	}
}

func TestFinanceTransactionCreateBlankOccurredAtMeansNow(t *testing.T) {
	service := &mcpFinanceServiceStub{txResult: &dto.TransactionResult{Transaction: financeTestTransaction()}}
	if _, err := FinanceTransactionCreate(mcpExternalContext(), financeResolver(), service, FinanceTransactionCreateInput{Space: "Family", Type: "expense", Amount: 1}); err != nil {
		t.Fatalf("finance_transaction_create: %v", err)
	}
	if !service.txCreate.OccurredAt.IsZero() {
		t.Fatalf("occurred_at = %v, want zero (service uses now)", service.txCreate.OccurredAt)
	}
}

func TestFinanceTransactionToolsRejectBadTimeAndUUIDBeforeService(t *testing.T) {
	ctx := mcpExternalContext()
	service := &mcpFinanceServiceStub{}
	bad := "2026-11-10 07:30"
	id := "00000000-0000-0000-0000-000000000001"
	_, err := FinanceTransactionCreate(ctx, financeResolver(), service, FinanceTransactionCreateInput{Space: "Family", OccurredAt: bad})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceTransactionList(ctx, financeResolver(), service, FinanceTransactionListInput{Space: "Family", From: bad})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceTransactionList(ctx, financeResolver(), service, FinanceTransactionListInput{Space: "Family", To: bad})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceTransactionUpdate(ctx, financeResolver(), service, FinanceTransactionUpdateInput{Space: "Family", ID: id, OccurredAt: &bad})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceTransactionUpdate(ctx, financeResolver(), service, FinanceTransactionUpdateInput{Space: "Family", ID: "not-a-uuid"})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceTransactionDelete(ctx, financeResolver(), service, FinanceTransactionDeleteInput{Space: "Family", ID: ""})
	assertMCPCode(t, err, "invalid_input")
	if service.calls != 0 {
		t.Fatal("invalid input reached the finance service")
	}
}

func TestCreateOutputCarriesDuplicateAndBudget(t *testing.T) {
	tx := financeTestTransaction()
	dup := financeTestTransaction()
	dup.ID = "tx-0"
	budget := domainfinance.BudgetStatus{CategoryID: "cat-1", Budget: 100000, Spent: 85000, Remaining: 15000, PercentUsed: 85}
	service := &mcpFinanceServiceStub{txResult: &dto.TransactionResult{Transaction: tx, PossibleDuplicate: &dup, BudgetStatus: &budget}}
	got, err := FinanceTransactionCreate(mcpExternalContext(), financeResolver(), service, FinanceTransactionCreateInput{Space: "Family"})
	if err != nil || got == nil {
		t.Fatalf("finance_transaction_create: %v", err)
	}
	out := encodeToMap(t, got)
	if out["id"] != "tx-1" || out["amount"] != float64(25000) || out["type"] != "expense" || out["account_id"] != "acc-1" || out["category_id"] != "cat-1" ||
		out["merchant"] != "Warung" || out["note"] != "lunch" || out["space_id"] != financeTestSpaceID {
		t.Fatalf("transaction fields = %v", out)
	}
	duplicate, _ := out["possible_duplicate"].(map[string]any)
	if duplicate["id"] != "tx-0" || duplicate["occurred_at"] != "2026-11-10T07:30:00+07:00" {
		t.Fatalf("possible_duplicate = %v", out["possible_duplicate"])
	}
	status, _ := out["budget_status"].(map[string]any)
	if status["percent_used"] != float64(85) || status["spent"] != float64(85000) || status["remaining"] != float64(15000) || status["budget"] != float64(100000) {
		t.Fatalf("budget_status = %v", out["budget_status"])
	}
}

func TestCreateOutputOmitsAbsentDuplicateAndBudgetAndRendersWIB(t *testing.T) {
	service := &mcpFinanceServiceStub{txResult: &dto.TransactionResult{Transaction: financeTestTransaction()}}
	got, err := FinanceTransactionCreate(mcpExternalContext(), financeResolver(), service, FinanceTransactionCreateInput{Space: "Family"})
	if err != nil {
		t.Fatalf("finance_transaction_create: %v", err)
	}
	out := encodeToMap(t, got)
	if _, ok := out["possible_duplicate"]; ok {
		t.Fatalf("possible_duplicate present: %v", out)
	}
	if _, ok := out["budget_status"]; ok {
		t.Fatalf("budget_status present: %v", out)
	}
	if out["occurred_at"] != "2026-11-10T07:30:00+07:00" {
		t.Fatalf("occurred_at = %v, want WIB", out["occurred_at"])
	}
	if out["created_at"] != "2026-11-10T00:30:00Z" {
		t.Fatalf("created_at = %v, want UTC", out["created_at"])
	}
}

func TestFinanceTransactionListMapsInputAndEncodes(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceTransactionList(mcpExternalContext(), financeResolver(), service, FinanceTransactionListInput{
		Space: "Family", Type: "expense", Category: "Makan", Account: "BCA", Search: "warung",
		From: "2026-11-01T00:00:00+07:00", To: "2026-12-01T00:00:00+07:00", Limit: 20,
	})
	if err != nil {
		t.Fatalf("finance_transaction_list: %v", err)
	}
	assertFinanceActor(t, service)
	in := service.txList
	if in.Space != financeTestSpaceID || in.Type != "expense" || in.Category != "Makan" || in.Account != "BCA" || in.Search != "warung" || in.Limit != 20 ||
		in.From == nil || !in.From.Equal(time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC)) ||
		in.To == nil || !in.To.Equal(time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("input = %+v", in)
	}
	if mustJSON(t, got) != `{"transactions":[]}` {
		t.Fatalf("empty list encodes as %s", mustJSON(t, got))
	}
}

func TestFinanceTransactionListNilBoundsAndOutput(t *testing.T) {
	service := &mcpFinanceServiceStub{txs: []domainfinance.Transaction{financeTestTransaction()}}
	got, err := FinanceTransactionList(mcpExternalContext(), financeResolver(), service, FinanceTransactionListInput{Space: "Family"})
	if err != nil || len(got.Transactions) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if service.txList.From != nil || service.txList.To != nil {
		t.Fatalf("bounds = %v %v, want nil", service.txList.From, service.txList.To)
	}
	if !strings.Contains(mustJSON(t, got), `"occurred_at":"2026-11-10T07:30:00+07:00"`) {
		t.Fatalf("list not rendered in WIB: %s", mustJSON(t, got))
	}
}

func TestFinanceTransactionUpdateMapsInput(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000042"
	service := &mcpFinanceServiceStub{txResult: &dto.TransactionResult{Transaction: financeTestTransaction()}}
	typ, amount, account, toAccount, category, merchant, note, at := "expense", int64(30000), "Cash", "", "", "Kantin", "snack", "2026-11-10T08:00:00+07:00"
	got, err := FinanceTransactionUpdate(mcpExternalContext(), financeResolver(), service, FinanceTransactionUpdateInput{
		Space: "Family", ID: " " + id + " ", Type: &typ, Amount: &amount, Account: &account, ToAccount: &toAccount,
		Category: &category, Merchant: &merchant, Note: &note, OccurredAt: &at,
	})
	if err != nil || got == nil {
		t.Fatalf("finance_transaction_update: %v", err)
	}
	assertFinanceActor(t, service)
	in := service.txUpdate
	if service.txSpaceID != financeTestSpaceID || service.txID != id ||
		in.Type == nil || *in.Type != "expense" || in.Amount == nil || *in.Amount != 30000 || in.Account == nil || *in.Account != "Cash" ||
		in.ToAccount == nil || *in.ToAccount != "" || in.Category == nil || *in.Category != "" ||
		in.Merchant == nil || *in.Merchant != "Kantin" || in.Note == nil || *in.Note != "snack" ||
		in.OccurredAt == nil || !in.OccurredAt.Equal(time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("id = %q, input = %+v", service.txID, in)
	}
}

func TestFinanceTransactionUpdateAbsentFieldsStayNil(t *testing.T) {
	service := &mcpFinanceServiceStub{txResult: &dto.TransactionResult{Transaction: financeTestTransaction()}}
	if _, err := FinanceTransactionUpdate(mcpExternalContext(), financeResolver(), service, FinanceTransactionUpdateInput{Space: "Family", ID: "00000000-0000-0000-0000-000000000042"}); err != nil {
		t.Fatalf("finance_transaction_update: %v", err)
	}
	if service.txUpdate != (dto.TransactionUpdateInput{}) {
		t.Fatalf("input = %+v, want all nil", service.txUpdate)
	}
}

func TestFinanceTransactionDeleteMapsInput(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000042"
	service := &mcpFinanceServiceStub{}
	got, err := FinanceTransactionDelete(mcpExternalContext(), financeResolver(), service, FinanceTransactionDeleteInput{Space: "Family", ID: id})
	if err != nil {
		t.Fatalf("finance_transaction_delete: %v", err)
	}
	assertFinanceActor(t, service)
	if service.txSpaceID != financeTestSpaceID || service.txID != id || !got.Deleted || got.ID != id {
		t.Fatalf("space = %s, id = %s, output = %+v", service.txSpaceID, service.txID, got)
	}
}

func TestFinanceTransactionDescriptionsGuideTheBot(t *testing.T) {
	d := financeDescriptions(t)
	assertDescriptionHas(t, d, "finance_transaction_create",
		"possible_duplicate", "double entry", "finance_transaction_delete", "percent_used", "80", "never compute", "confirm with the user",
		"photo", "voice", "to_account", "no category", "income", "expense", "+07:00", "blank occurred_at", "own transactions", "ask an admin", "already saved")
	assertDescriptionHas(t, d, "finance_transaction_list", "finance_account_list", "finance_category_list", "RFC3339", "never compute")
	assertDescriptionHas(t, d, "finance_transaction_update", "empty string", "clears the category", "unchanged", "RFC3339")
	assertDescriptionHas(t, d, "finance_transaction_delete", "owner or admin", "ask an admin")
}
