package mcp

import (
	"strings"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
)

func financeTestRecurring() *domainfinance.Recurring {
	end := time.Date(2027, 11, 9, 17, 0, 0, 0, time.UTC)
	day, reason := 10, "account archived"
	return &domainfinance.Recurring{
		ID: "r-1", SpaceID: financeTestSpaceID, Type: "expense", Amount: 500000, AccountID: "acc-1", Frequency: "monthly", IntervalN: 1, DayOfMonth: &day,
		StartAt: time.Date(2026, 11, 10, 0, 30, 0, 0, time.UTC), NextRunAt: time.Date(2026, 12, 10, 0, 30, 0, 0, time.UTC), EndAt: &end,
		Paused: true, PauseReason: &reason, CreatedAt: financeTestOccurredAt, UpdatedAt: financeTestOccurredAt,
	}
}

func TestFinanceRecurringCreateMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{recResult: financeTestRecurring()}
	day := 10
	got, err := FinanceRecurringCreate(mcpExternalContext(), financeResolver(), service, FinanceRecurringCreateInput{
		Space: "Family", Type: "transfer", Amount: 500000, Account: "BCA", ToAccount: "Cash", Category: "Tabungan", Merchant: "Kos", Note: "rent",
		Frequency: "monthly", IntervalN: 2, DayOfMonth: &day, StartAt: "2026-11-10T07:30:00+07:00", EndAt: "2027-11-10T00:00:00+07:00",
	})
	if err != nil {
		t.Fatalf("finance_recurring_create: %v", err)
	}
	assertFinanceActor(t, service)
	in := service.recCreate
	if in.Space != financeTestSpaceID || in.Type != "transfer" || in.Amount != 500000 || in.Account != "BCA" || in.ToAccount != "Cash" || in.Category != "Tabungan" ||
		in.Merchant != "Kos" || in.Note != "rent" || in.Frequency != "monthly" || in.IntervalN != 2 || in.DayOfMonth == nil || *in.DayOfMonth != 10 ||
		!in.StartAt.Equal(financeTestOccurredAt) || in.EndAt == nil || !in.EndAt.Equal(time.Date(2027, 11, 9, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("input = %+v", in)
	}
	out := encodeToMap(t, got)
	if out["start_at"] != "2026-11-10T07:30:00+07:00" || out["next_run_at"] != "2026-12-10T07:30:00+07:00" || out["end_at"] != "2027-11-10T00:00:00+07:00" ||
		out["created_at"] != "2026-11-10T00:30:00Z" || out["pause_reason"] != "account archived" || out["paused"] != true || out["day_of_month"] != float64(10) ||
		out["amount"] != float64(500000) || out["frequency"] != "monthly" {
		t.Fatalf("output = %v", out)
	}
}

func TestFinanceRecurringCreateNilOptionals(t *testing.T) {
	r := financeTestRecurring()
	r.EndAt, r.DayOfMonth, r.PauseReason, r.Paused = nil, nil, nil, false
	service := &mcpFinanceServiceStub{recResult: r}
	got, err := FinanceRecurringCreate(mcpExternalContext(), financeResolver(), service, FinanceRecurringCreateInput{Space: "Family", StartAt: "2026-11-10T07:30:00+07:00"})
	if err != nil {
		t.Fatalf("finance_recurring_create: %v", err)
	}
	if service.recCreate.EndAt != nil || service.recCreate.DayOfMonth != nil {
		t.Fatalf("input = %+v", service.recCreate)
	}
	out := encodeToMap(t, got)
	for _, key := range []string{"end_at", "day_of_month", "pause_reason"} {
		if _, ok := out[key]; ok {
			t.Errorf("%s present: %v", key, out)
		}
	}
}

func TestFinanceRecurringRejectsBadInputBeforeService(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	ctx := mcpExternalContext()
	id := "00000000-0000-0000-0000-000000000042"
	_, err := FinanceRecurringCreate(ctx, financeResolver(), service, FinanceRecurringCreateInput{Space: "Family", StartAt: ""})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceRecurringCreate(ctx, financeResolver(), service, FinanceRecurringCreateInput{Space: "Family", StartAt: "2026-11-10T07:30:00+07:00", EndAt: "tomorrow"})
	assertMCPCode(t, err, "invalid_input")
	bad := "tomorrow"
	_, err = FinanceRecurringUpdate(ctx, financeResolver(), service, FinanceRecurringUpdateInput{Space: "Family", ID: id, StartAt: &bad})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceRecurringUpdate(ctx, financeResolver(), service, FinanceRecurringUpdateInput{Space: "Family", ID: id, EndAt: &bad})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceRecurringUpdate(ctx, financeResolver(), service, FinanceRecurringUpdateInput{Space: "Family", ID: "x"})
	assertMCPCode(t, err, "invalid_input")
	_, err = FinanceRecurringDelete(ctx, financeResolver(), service, FinanceRecurringDeleteInput{Space: "Family", ID: "x"})
	assertMCPCode(t, err, "invalid_input")
	if service.calls != 0 {
		t.Fatal("invalid input reached the finance service")
	}
}

func TestFinanceRecurringListMapsInputAndEncodes(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceRecurringList(mcpExternalContext(), financeResolver(), service, FinanceRecurringListInput{Space: "Family", Limit: 7})
	if err != nil {
		t.Fatalf("finance_recurring_list: %v", err)
	}
	assertFinanceActor(t, service)
	if service.recList != (dto.RecurringListInput{Space: financeTestSpaceID, Limit: 7}) {
		t.Fatalf("input = %+v", service.recList)
	}
	if mustJSON(t, got) != `{"recurring":[]}` {
		t.Fatalf("empty list encodes as %s", mustJSON(t, got))
	}
	service.recs = []domainfinance.Recurring{*financeTestRecurring()}
	got, _ = FinanceRecurringList(mcpExternalContext(), financeResolver(), service, FinanceRecurringListInput{Space: "Family"})
	if !strings.Contains(mustJSON(t, got), `"next_run_at":"2026-12-10T07:30:00+07:00"`) {
		t.Fatalf("list not rendered in WIB: %s", mustJSON(t, got))
	}
}

func TestFinanceRecurringUpdateMapsInput(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000042"
	service := &mcpFinanceServiceStub{recResult: financeTestRecurring()}
	typ, amount, account, toAccount, category, merchant, note, freq := "income", int64(750000), "Cash", "", "", "Gaji", "salary", "weekly"
	interval, day, paused := 3, 5, false
	start, end := "2026-11-10T08:00:00+07:00", "2027-01-01T00:00:00+07:00"
	_, err := FinanceRecurringUpdate(mcpExternalContext(), financeResolver(), service, FinanceRecurringUpdateInput{
		Space: "Family", ID: id, Type: &typ, Amount: &amount, Account: &account, ToAccount: &toAccount, Category: &category, Merchant: &merchant, Note: &note,
		Frequency: &freq, IntervalN: &interval, DayOfMonth: &day, StartAt: &start, EndAt: &end, ClearEndAt: true, Paused: &paused,
	})
	if err != nil {
		t.Fatalf("finance_recurring_update: %v", err)
	}
	assertFinanceActor(t, service)
	in := service.recUpdate
	if in.Space != financeTestSpaceID || in.ID != id || in.Type == nil || *in.Type != "income" || in.Amount == nil || *in.Amount != 750000 ||
		in.Account == nil || *in.Account != "Cash" || in.ToAccount == nil || *in.ToAccount != "" || in.Category == nil || *in.Category != "" ||
		in.Merchant == nil || *in.Merchant != "Gaji" || in.Note == nil || *in.Note != "salary" || in.Frequency == nil || *in.Frequency != "weekly" ||
		in.IntervalN == nil || *in.IntervalN != 3 || in.DayOfMonth == nil || *in.DayOfMonth != 5 ||
		in.StartAt == nil || !in.StartAt.Equal(time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)) ||
		in.EndAt == nil || !in.EndAt.Equal(time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)) ||
		!in.ClearEndAt || in.Paused == nil || *in.Paused {
		t.Fatalf("input = %+v", in)
	}
}

func TestFinanceRecurringUpdateAbsentFieldsStayNil(t *testing.T) {
	service := &mcpFinanceServiceStub{recResult: financeTestRecurring()}
	id := "00000000-0000-0000-0000-000000000042"
	if _, err := FinanceRecurringUpdate(mcpExternalContext(), financeResolver(), service, FinanceRecurringUpdateInput{Space: "Family", ID: id}); err != nil {
		t.Fatalf("finance_recurring_update: %v", err)
	}
	if service.recUpdate != (dto.RecurringUpdateInput{Space: financeTestSpaceID, ID: id}) {
		t.Fatalf("input = %+v, want only space and id", service.recUpdate)
	}
}

func TestFinanceRecurringDeleteMapsInput(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000042"
	service := &mcpFinanceServiceStub{}
	got, err := FinanceRecurringDelete(mcpExternalContext(), financeResolver(), service, FinanceRecurringDeleteInput{Space: "Family", ID: id})
	if err != nil || !got.Deleted || got.ID != id {
		t.Fatalf("finance_recurring_delete: %v, %+v", err, got)
	}
	assertFinanceActor(t, service)
	if service.recDelete != (dto.RecurringDeleteInput{Space: financeTestSpaceID, ID: id}) {
		t.Fatalf("input = %+v", service.recDelete)
	}
}

func TestFinanceRecurringDescriptionsGuideTheBot(t *testing.T) {
	d := financeDescriptions(t)
	for _, name := range []string{"finance_recurring_create", "finance_recurring_update", "finance_recurring_delete"} {
		assertDescriptionHas(t, d, name, "owner or admin", "ask an admin")
	}
	assertDescriptionHas(t, d, "finance_recurring_create", "24 hours in the past", "day_of_month", "monthly", "on or after start_at", "WIB", "to_account", "no category")
	assertDescriptionHas(t, d, "finance_recurring_list", "pause_reason", "needs attention", "tell the admin")
	assertDescriptionHas(t, d, "finance_recurring_update", "skips missed", "clear_end_at", "empty string")
}
