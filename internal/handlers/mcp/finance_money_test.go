package mcp

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
	"family-assistant/pkg/config"
)

// These tests drive the finance tools through the real MCP SDK (HTTP
// tools/call and tools/list). That is the only path that proves the SDK's
// float64 round trip of arguments and its output-schema validation leave
// exact decimal money intact.

const (
	moneyTestTxID = "00000000-0000-0000-0000-0000000000a1"

	reasonPrecision = "at most 2 decimal places, no rounding is done"
	reasonFormat    = "use a plain decimal like 1005422.44, without thousands separators"
	reasonTooLarge  = "exceeds the maximum of 10000000000.00"
	reasonPositive  = "greater than 0"
)

func financeMoneyServer(t *testing.T, stub *mcpFinanceServiceStub) *httptest.Server {
	t.Helper()
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, financeResolver(), nil, nil, nil, nil, ToolServices{Finance: stub})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	return server
}

func callFinanceTool(t *testing.T, server *httptest.Server, name, args string) mcpServerResponse {
	t.Helper()
	return callMCPServer(t, server.URL, "profile", "secret", "tools/call", map[string]any{"name": name, "arguments": json.RawMessage(args)})
}

func moneyStub() *mcpFinanceServiceStub {
	tx := financeTestTransaction()
	return &mcpFinanceServiceStub{
		txResult:     &dto.TransactionResult{Transaction: tx},
		budgetResult: &domainfinance.Budget{ID: "b-1", SpaceID: financeTestSpaceID, CategoryID: "cat-1", Month: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Amount: 1},
		recResult:    financeTestRecurring(),
	}
}

// moneyTool describes one money-carrying tool input: args has a %s where the
// raw JSON value of the money field goes.
type moneyTool struct {
	name, field, args string
	sen               func(*mcpFinanceServiceStub) (domainfinance.Money, bool)
	allowNegative     bool // opening balance: the sign rule is the service's, not the boundary's
}

func moneyTools() []moneyTool {
	return []moneyTool{
		{name: "finance_transaction_create", field: "amount", args: `{"space":"Family","type":"expense","account":"BCA","amount":%s}`,
			sen: func(s *mcpFinanceServiceStub) (domainfinance.Money, bool) { return s.txCreate.Amount, true }},
		{name: "finance_transaction_update", field: "amount", args: `{"space":"Family","id":"` + moneyTestTxID + `","amount":%s}`,
			sen: func(s *mcpFinanceServiceStub) (domainfinance.Money, bool) { return ptrMoney(s.txUpdate.Amount) }},
		{name: "finance_account_create", field: "opening_balance", allowNegative: true, args: `{"space":"Family","name":"BCA","type":"credit_card","opening_balance":%s}`,
			sen: func(s *mcpFinanceServiceStub) (domainfinance.Money, bool) {
				return s.accountCreate.OpeningBalance, true
			}},
		{name: "finance_budget_set", field: "amount", args: `{"space":"Family","category":"Makan","month":"2026-11","amount":%s}`,
			sen: func(s *mcpFinanceServiceStub) (domainfinance.Money, bool) { return s.budgetSet.Amount, true }},
		{name: "finance_recurring_create", field: "amount", args: `{"space":"Family","type":"expense","account":"BCA","frequency":"monthly","start_at":"2026-11-10T07:30:00+07:00","amount":%s}`,
			sen: func(s *mcpFinanceServiceStub) (domainfinance.Money, bool) { return s.recCreate.Amount, true }},
		{name: "finance_recurring_update", field: "amount", args: `{"space":"Family","id":"` + moneyTestTxID + `","amount":%s}`,
			sen: func(s *mcpFinanceServiceStub) (domainfinance.Money, bool) { return ptrMoney(s.recUpdate.Amount) }},
	}
}

func ptrMoney(m *domainfinance.Money) (domainfinance.Money, bool) {
	if m == nil {
		return 0, false
	}
	return *m, true
}

func toolText(resp mcpServerResponse) string {
	if resp.Result == nil || len(resp.Result.Content) == 0 {
		return ""
	}
	return resp.Result.Content[0].Text
}

func TestFinanceMoneyInputsAcceptNumberAndStringExactly(t *testing.T) {
	accepted := []struct {
		raw  string
		want domainfinance.Money
	}{
		{`1005422.44`, 100542244},
		{`"1005422.44"`, 100542244},
		{`25000`, 2500000},
		{`"25000"`, 2500000},
		{`0.01`, 1},
		{`"0.1"`, 10},
		{`10000000000`, domainfinance.MaxAmount},
		{`"10000000000.00"`, domainfinance.MaxAmount},
		// An exponent form cannot be told apart from 100 once the SDK has decoded the
		// argument to float64: it is accepted as 100 (documented, not a defect).
		{`1e2`, 10000},
	}
	for _, tool := range moneyTools() {
		for _, c := range accepted {
			t.Run(tool.name+"/"+c.raw, func(t *testing.T) {
				stub := moneyStub()
				resp := callFinanceTool(t, financeMoneyServer(t, stub), tool.name, fmt.Sprintf(tool.args, c.raw))
				if resp.Error != nil || resp.Result == nil || resp.Result.IsError {
					t.Fatalf("response = %+v, text %q", resp, toolText(resp))
				}
				got, present := tool.sen(stub)
				if !present || got != c.want || stub.calls != 1 {
					t.Fatalf("%s %s: dto got %d (present %v, calls %d), want %d", tool.name, c.raw, got, present, stub.calls, c.want)
				}
			})
		}
	}
}

func TestFinanceMoneyInputsRejectBadValuesNamingTheField(t *testing.T) {
	rejected := []struct{ raw, reason string }{
		{`0.005`, reasonPrecision},
		{`"1.005"`, reasonPrecision},
		{`"1,5"`, reasonFormat},
		{`"1.005.422,44"`, reasonFormat},
		{`"abc"`, reasonFormat},
		{`" 5"`, reasonFormat},
		{`"+5"`, reasonFormat},
		{`"1e3"`, reasonFormat},
		{`10000000000.01`, reasonTooLarge},
		{`"10000000000.01"`, reasonTooLarge},
		{`99999999999999999999`, reasonTooLarge},
		{`-10000000000.01`, reasonTooLarge},
	}
	positiveOnly := []struct{ raw, reason string }{
		{`-5`, reasonPositive}, {`"-5"`, reasonPositive}, {`-0.01`, reasonPositive}, {`0`, reasonPositive}, {`"0"`, reasonPositive}, {`""`, reasonFormat},
	}
	for _, tool := range moneyTools() {
		cases := rejected
		if !tool.allowNegative {
			cases = append(append([]struct{ raw, reason string }{}, rejected...), positiveOnly...)
		}
		for _, c := range cases {
			t.Run(tool.name+"/"+c.raw, func(t *testing.T) {
				stub := moneyStub()
				resp := callFinanceTool(t, financeMoneyServer(t, stub), tool.name, fmt.Sprintf(tool.args, c.raw))
				text := toolText(resp)
				if resp.Result == nil || !resp.Result.IsError || !strings.Contains(text, "invalid input: "+tool.field+" ") || !strings.Contains(text, c.reason) {
					t.Fatalf("%s %s: response %+v, text %q; want invalid input on %s with %q", tool.name, c.raw, resp, text, tool.field, c.reason)
				}
				if stub.calls != 0 {
					t.Fatalf("%s %s reached the service", tool.name, c.raw)
				}
			})
		}
	}
}

func TestFinanceOpeningBalanceMayBeNegativeAtTheBoundary(t *testing.T) {
	stub := moneyStub()
	resp := callFinanceTool(t, financeMoneyServer(t, stub), "finance_account_create", `{"space":"Family","name":"Visa","type":"credit_card","opening_balance":"-1500.50"}`)
	if resp.Result == nil || resp.Result.IsError || stub.accountCreate.OpeningBalance != -150050 {
		t.Fatalf("response %+v text %q, opening balance %d", resp, toolText(resp), stub.accountCreate.OpeningBalance)
	}
}

func TestFinanceMoneyInputsRejectNonNumberNonString(t *testing.T) {
	for _, tool := range moneyTools() {
		for _, raw := range []string{`true`, `[]`, `{}`} {
			t.Run(tool.name+"/"+raw, func(t *testing.T) {
				stub := moneyStub()
				resp := callFinanceTool(t, financeMoneyServer(t, stub), tool.name, fmt.Sprintf(tool.args, raw))
				if resp.Result == nil || !resp.Result.IsError || stub.calls != 0 {
					t.Fatalf("%s %s: response %+v, calls %d; want a schema rejection", tool.name, raw, resp, stub.calls)
				}
			})
		}
	}
}

// An explicit JSON null on a pointer update field behaves like an omitted one.
func TestFinanceMoneyNullUpdateFieldMeansUnchanged(t *testing.T) {
	for _, tool := range moneyTools() {
		if tool.name != "finance_transaction_update" && tool.name != "finance_recurring_update" {
			continue
		}
		stub := moneyStub()
		resp := callFinanceTool(t, financeMoneyServer(t, stub), tool.name, fmt.Sprintf(tool.args, "null"))
		if resp.Result == nil || resp.Result.IsError || stub.calls != 1 {
			t.Fatalf("%s: response %+v text %q calls %d", tool.name, resp, toolText(resp), stub.calls)
		}
		if got, present := tool.sen(stub); present {
			t.Fatalf("%s: null amount reached the service as %d, want nil (unchanged)", tool.name, got)
		}
	}
}

// structured returns the structuredContent text the SDK sent for a successful call.
func structured(t *testing.T, resp mcpServerResponse) string {
	t.Helper()
	if resp.Error != nil || resp.Result == nil || resp.Result.IsError || len(resp.Result.StructuredOutput) == 0 {
		t.Fatalf("call failed: %+v text %q", resp, toolText(resp))
	}
	return string(resp.Result.StructuredOutput)
}

func assertContainsAll(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %s:\n%s", want, text)
		}
	}
}

func TestFinanceOutputsRenderExactDecimalStrings(t *testing.T) {
	tx := financeTestTransaction()
	tx.Amount = 100542244
	dup := financeTestTransaction()
	dup.ID = "tx-0"
	budget := domainfinance.BudgetStatus{CategoryID: "cat-1", Budget: 1000000, Spent: 1000050, Remaining: -50, PercentUsed: 100}
	stub := moneyStub()
	stub.txResult = &dto.TransactionResult{Transaction: tx, PossibleDuplicate: &dup, BudgetStatus: &budget}
	stub.txs = []domainfinance.Transaction{tx}
	stub.accounts = []domainfinance.Account{{ID: "a1", Name: "BCA", Type: "bank", OpeningBalance: 100542244}}
	stub.budgetResult.Amount = 100542244
	stub.budgetStatus = []dto.BudgetStatusView{{BudgetStatus: budget, CategoryName: "Makan"}}
	stub.recResult.Amount = 100542244
	stub.balances = &dto.BalancesReport{
		Accounts: []domainfinance.AccountBalance{{AccountID: "a1", Name: "BCA", Type: "bank", Balance: 100542244}, {AccountID: "a2", Name: "Visa", Type: "credit_card", Balance: -50}},
		Total:    100542194,
	}
	stub.summary = &dto.SummaryReport{
		Period: "month", From: financeTestOccurredAt, To: financeTestOccurredAt.Add(time.Hour),
		Income: 100542244, Expense: 2500000, Net: 98042244, PreviousIncome: 50, PreviousExpense: 0,
		ByCategory:   []domainfinance.CategoryTotal{{CategoryID: "c1", Name: "Makan", Total: 2500000}},
		ByAccount:    []domainfinance.AccountTotal{{AccountID: "a1", Name: "BCA", Income: 100542244, Expense: 1}},
		TopMerchants: []domainfinance.MerchantTotal{{Merchant: "Warung", Total: 1, Count: 1}},
	}
	server := financeMoneyServer(t, stub)

	create := structured(t, callFinanceTool(t, server, "finance_transaction_create", `{"space":"Family","type":"expense","account":"BCA","amount":"1005422.44"}`))
	assertContainsAll(t, create, `"amount":"1005422.44"`, `"amount":"25000"`, `"budget":"10000"`, `"spent":"10000.50"`, `"remaining":"-0.50"`, `"percent_used":100`)
	assertContainsAll(t, structured(t, callFinanceTool(t, server, "finance_transaction_list", `{"space":"Family"}`)), `"amount":"1005422.44"`)
	assertContainsAll(t, structured(t, callFinanceTool(t, server, "finance_account_list", `{"space":"Family"}`)), `"opening_balance":"1005422.44"`)
	assertContainsAll(t, structured(t, callFinanceTool(t, server, "finance_budget_set", `{"space":"Family","category":"Makan","month":"2026-11","amount":"1005422.44"}`)), `"amount":"1005422.44"`)
	assertContainsAll(t, structured(t, callFinanceTool(t, server, "finance_budget_list", `{"space":"Family"}`)), `"budget":"10000"`, `"spent":"10000.50"`, `"remaining":"-0.50"`)
	assertContainsAll(t, structured(t, callFinanceTool(t, server, "finance_recurring_create", fmt.Sprintf(moneyTools()[4].args, `1005422.44`))), `"amount":"1005422.44"`)
	assertContainsAll(t, structured(t, callFinanceTool(t, server, "finance_balances", `{"space":"Family"}`)), `"balance":"1005422.44"`, `"balance":"-0.50"`, `"total":"1005421.94"`)
	assertContainsAll(t, structured(t, callFinanceTool(t, server, "finance_summary", `{"space":"Family"}`)),
		`"income":"1005422.44"`, `"expense":"25000"`, `"net":"980422.44"`, `"previous_income":"0.50"`, `"previous_expense":"0"`,
		`"total":"25000"`, `"expense":"0.01"`, `"total":"0.01"`)
}

func TestFinanceMoneyMarshalsAsJSONString(t *testing.T) {
	out := FinanceBalancesOutput{Accounts: []domainfinance.AccountBalance{{AccountID: "a", Balance: 50}}, Total: -1}
	if got := mustJSON(t, out); got != `{"accounts":[{"account_id":"a","name":"","type":"","balance":"0.50"}],"total":"-0.01"}` {
		t.Fatalf("balances JSON = %s", got)
	}
}

// walkSchema calls fn for every named property of every (nested) object schema.
func walkSchema(node any, fn func(name string, prop map[string]any)) {
	switch v := node.(type) {
	case map[string]any:
		if props, ok := v["properties"].(map[string]any); ok {
			for name, p := range props {
				if pm, ok := p.(map[string]any); ok {
					fn(name, pm)
				}
			}
		}
		for _, child := range v {
			walkSchema(child, fn)
		}
	case []any:
		for _, child := range v {
			walkSchema(child, fn)
		}
	}
}

type financeToolSchemas struct {
	Name, Description string
	Input, Output     map[string]any
}

func financeToolSchemaList(t *testing.T) []financeToolSchemas {
	t.Helper()
	server := financeMoneyServer(t, moneyStub())
	resp := callMCPServer(t, server.URL, "profile", "secret", "tools/list", map[string]any{})
	if resp.Error != nil || resp.Result == nil {
		t.Fatalf("tools/list: %+v", resp)
	}
	var tools []financeToolSchemas
	for _, tool := range resp.Result.Tools {
		if !strings.HasPrefix(tool.Name, "finance_") {
			continue
		}
		entry := financeToolSchemas{Name: tool.Name, Description: tool.Description}
		if err := json.Unmarshal(tool.InputSchema, &entry.Input); err != nil {
			t.Fatalf("%s input schema: %v", tool.Name, err)
		}
		if err := json.Unmarshal(tool.OutputSchema, &entry.Output); err != nil {
			t.Fatalf("%s output schema: %v", tool.Name, err)
		}
		tools = append(tools, entry)
	}
	if len(tools) != 19 {
		t.Fatalf("got %d finance tools, want 19", len(tools))
	}
	return tools
}

func schemaTypes(prop map[string]any) []string {
	var types []string
	if s, ok := prop["type"].(string); ok {
		types = append(types, s)
	}
	if list, ok := prop["type"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				types = append(types, s)
			}
		}
	}
	return types
}

func hasAll(have []string, wants ...string) bool {
	for _, want := range wants {
		found := false
		for _, h := range have {
			found = found || h == want
		}
		if !found {
			return false
		}
	}
	return true
}

var moneyOutputFields = map[string]bool{
	"amount": true, "opening_balance": true, "balance": true, "total": true, "income": true, "expense": true, "net": true,
	"previous_income": true, "previous_expense": true, "budget": true, "spent": true, "remaining": true,
}

func TestFinanceOutputSchemasDeclareMoneyAsDecimalString(t *testing.T) {
	seen := map[string]int{}
	for _, tool := range financeToolSchemaList(t) {
		walkSchema(tool.Output, func(name string, prop map[string]any) {
			if !moneyOutputFields[name] {
				return
			}
			seen[name]++
			if got := schemaTypes(prop); len(got) != 1 || got[0] != "string" {
				t.Errorf("%s output %s has type %v, want string", tool.Name, name, got)
			}
			if pattern, _ := prop["pattern"].(string); pattern == "" {
				t.Errorf("%s output %s has no decimal pattern", tool.Name, name)
			}
		})
	}
	for name := range moneyOutputFields {
		if seen[name] == 0 {
			t.Errorf("no output schema carries money field %s", name)
		}
	}
}

func TestFinanceInputSchemasAcceptNumberOrStringForMoney(t *testing.T) {
	byName := map[string]financeToolSchemas{}
	for _, tool := range financeToolSchemaList(t) {
		byName[tool.Name] = tool
	}
	for _, tool := range moneyTools() {
		found := false
		walkSchema(byName[tool.name].Input, func(name string, prop map[string]any) {
			if name != tool.field {
				return
			}
			found = true
			if types := schemaTypes(prop); !hasAll(types, "number", "string") || hasAll(types, "integer") || hasAll(types, "boolean") {
				t.Errorf("%s.%s type = %v, want number and string", tool.name, name, types)
			}
		})
		if !found {
			t.Errorf("%s has no %s input property", tool.name, tool.field)
		}
	}
}

func TestFinanceDescriptionsDescribeDecimalRupiah(t *testing.T) {
	for _, tool := range financeToolSchemaList(t) {
		for _, stale := range []string{"whole rupiah", "integer", "never a string", "never decimals"} {
			if strings.Contains(strings.ToLower(tool.Description), stale) {
				t.Errorf("%s description still says %q", tool.Name, stale)
			}
		}
		assertContainsAll(t, tool.Description, "two decimals", "25000", "1005422.44", "decimal string")
		var inputs []string
		walkSchema(tool.Input, func(name string, prop map[string]any) {
			desc, _ := prop["description"].(string)
			if strings.Contains(strings.ToLower(desc), "whole rupiah") {
				t.Errorf("%s.%s description still says whole rupiah: %s", tool.Name, name, desc)
			}
			if name == "amount" || name == "opening_balance" {
				inputs = append(inputs, desc)
				assertContainsAll(t, desc, "two decimals", "25000", "1005422.44")
				if strings.Contains(strings.ToLower(desc), "integer") {
					t.Errorf("%s.%s description still says integer: %s", tool.Name, name, desc)
				}
			}
		})
		walkSchema(tool.Output, func(name string, prop map[string]any) {
			if moneyOutputFields[name] {
				desc, _ := prop["description"].(string)
				if !strings.Contains(desc, "decimal string") {
					t.Errorf("%s output %s description = %q, want it to say decimal string", tool.Name, name, desc)
				}
			}
		})
		if strings.Contains(tool.Name, "_update") && (strings.Contains(tool.Name, "transaction") || strings.Contains(tool.Name, "recurring")) {
			for _, desc := range inputs {
				assertContainsAll(t, desc, "null")
			}
		}
	}
	for _, tool := range financeToolSchemaList(t) {
		if tool.Name == "finance_budget_list" {
			assertContainsAll(t, tool.Description, "remaining", "negative")
		}
	}
}

// Direct handler calls (no SDK in between): the text of a MoneyInput is parsed
// exactly, with no float anywhere.
func TestFinanceHandlersParseMoneyInputExactly(t *testing.T) {
	for in, want := range map[MoneyInput]domainfinance.Money{"1005422.44": 100542244, "25000": 2500000, "0.50": 50} {
		service := moneyStub()
		if _, err := FinanceBudgetSet(mcpExternalContext(), financeResolver(), service, FinanceBudgetSetInput{Space: "Family", Category: "Makan", Month: "2026-11", Amount: in}); err != nil {
			t.Fatalf("budget set %q: %v", in, err)
		}
		if service.budgetSet.Amount != want {
			t.Fatalf("%q -> %d sen, want %d", in, service.budgetSet.Amount, want)
		}
	}
	service := moneyStub()
	bad := MoneyInput("1.005")
	_, err := FinanceTransactionUpdate(mcpExternalContext(), financeResolver(), service, FinanceTransactionUpdateInput{Space: "Family", ID: moneyTestTxID, Amount: &bad})
	assertMCPCode(t, err, "invalid_input")
	if service.calls != 0 || !strings.Contains(err.Error(), "amount") || !strings.Contains(err.Error(), reasonPrecision) {
		t.Fatalf("calls %d, err %v", service.calls, err)
	}
}

func TestMoneyInputUnmarshalKeepsTheText(t *testing.T) {
	var in struct {
		A MoneyInput  `json:"a"`
		B MoneyInput  `json:"b"`
		C *MoneyInput `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"a":1005422.44,"b":"0.50","c":null}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.A != "1005422.44" || in.B != "0.50" || in.C != nil {
		t.Fatalf("got %+v", in)
	}
}
