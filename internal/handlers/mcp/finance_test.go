package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/config"
)

// mcpFinanceServiceStub implements only the account and category methods; the
// embedded nil interface panics if a tool reaches anything else.
type mcpFinanceServiceStub struct {
	interfacefinance.ServiceFinanceInterface
	err error

	actor domainidentity.ActorContext
	calls int

	accountCreate  dto.AccountCreateInput
	accountList    dto.AccountListInput
	accountUpdate  dto.AccountUpdateInput
	categoryCreate dto.CategoryCreateInput
	categoryList   dto.CategoryListInput
	categoryUpdate dto.CategoryUpdateInput

	accounts   []domainfinance.Account
	categories []domainfinance.Category
}

func (s *mcpFinanceServiceStub) hit(actor domainidentity.ActorContext) {
	s.calls++
	s.actor = actor
}

func (s *mcpFinanceServiceStub) AccountCreate(_ context.Context, actor domainidentity.ActorContext, in dto.AccountCreateInput) (*domainfinance.Account, error) {
	s.hit(actor)
	s.accountCreate = in
	return &domainfinance.Account{ID: "acc-1", SpaceID: in.Space, Name: in.Name, Type: in.Type, OpeningBalance: in.OpeningBalance}, s.err
}

func (s *mcpFinanceServiceStub) AccountList(_ context.Context, actor domainidentity.ActorContext, in dto.AccountListInput) ([]domainfinance.Account, error) {
	s.hit(actor)
	s.accountList = in
	return s.accounts, s.err
}

func (s *mcpFinanceServiceStub) AccountUpdate(_ context.Context, actor domainidentity.ActorContext, in dto.AccountUpdateInput) (*domainfinance.Account, error) {
	s.hit(actor)
	s.accountUpdate = in
	return &domainfinance.Account{ID: in.ID, SpaceID: in.Space, Name: "BCA"}, s.err
}

func (s *mcpFinanceServiceStub) CategoryCreate(_ context.Context, actor domainidentity.ActorContext, in dto.CategoryCreateInput) (*domainfinance.Category, error) {
	s.hit(actor)
	s.categoryCreate = in
	return &domainfinance.Category{ID: "cat-1", SpaceID: in.Space, Name: in.Name, Kind: in.Kind}, s.err
}

func (s *mcpFinanceServiceStub) CategoryList(_ context.Context, actor domainidentity.ActorContext, in dto.CategoryListInput) ([]domainfinance.Category, error) {
	s.hit(actor)
	s.categoryList = in
	return s.categories, s.err
}

func (s *mcpFinanceServiceStub) CategoryUpdate(_ context.Context, actor domainidentity.ActorContext, in dto.CategoryUpdateInput) (*domainfinance.Category, error) {
	s.hit(actor)
	s.categoryUpdate = in
	return &domainfinance.Category{ID: in.ID, SpaceID: in.Space, Name: "Food", Kind: "expense"}, s.err
}

const financeTestSpaceID = "00000000-0000-0000-0000-000000000501"

func financeResolver() *mcpExternalResolverStub {
	return &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", financeTestSpaceID, "Family", domainspace.TypeShared, "role-admin"),
		}},
		permissions: []domainpermission.Permission{{Resource: "finance", Action: "create"}},
	}
}

func assertMCPCode(t *testing.T, err error, code string) {
	t.Helper()
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != code {
		t.Fatalf("error = %T %v, want code %s", err, err, code)
	}
}

func TestFinanceAccountCreateMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceAccountCreate(mcpExternalContext(), financeResolver(), service, FinanceAccountCreateInput{
		Space: " family ", Name: "BCA", Type: "bank", OpeningBalance: 1500000,
	})
	if err != nil {
		t.Fatalf("finance_account_create: %v", err)
	}
	want := dto.AccountCreateInput{Space: financeTestSpaceID, Name: "BCA", Type: "bank", OpeningBalance: 1500000}
	if service.accountCreate != want || service.actor.SpaceID != financeTestSpaceID || got == nil || got.ID != "acc-1" || got.OpeningBalance != 1500000 {
		t.Fatalf("input = %+v, output = %+v, actor space = %s", service.accountCreate, got, service.actor.SpaceID)
	}
}

func TestFinanceAccountListMapsInputAndEncodesEmptyAsArray(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceAccountList(mcpExternalContext(), financeResolver(), service, FinanceAccountListInput{Space: "Family", IncludeArchived: true, Limit: 20})
	if err != nil {
		t.Fatalf("finance_account_list: %v", err)
	}
	if service.accountList != (dto.AccountListInput{Space: financeTestSpaceID, IncludeArchived: true, Limit: 20}) {
		t.Fatalf("input = %+v", service.accountList)
	}
	encoded, err := json.Marshal(FinanceAccountListOutput{Accounts: got})
	if err != nil || string(encoded) != `{"accounts":[]}` {
		t.Fatalf("empty list encodes as %s (%v)", encoded, err)
	}
}

func TestFinanceAccountListReturnsServiceOutput(t *testing.T) {
	service := &mcpFinanceServiceStub{accounts: []domainfinance.Account{{ID: "a1", Name: "Cash", Type: "cash", OpeningBalance: 25000}}}
	got, err := FinanceAccountList(mcpExternalContext(), financeResolver(), service, FinanceAccountListInput{Space: "Family"})
	if err != nil || len(got) != 1 || got[0].ID != "a1" {
		t.Fatalf("got %+v, %v", got, err)
	}
	encoded, _ := json.Marshal(got[0])
	if !strings.Contains(string(encoded), `"opening_balance":25000`) {
		t.Fatalf("money is not a JSON integer: %s", encoded)
	}
}

func TestFinanceAccountUpdateMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	name, archived := "BCA Utama", true
	got, err := FinanceAccountUpdate(mcpExternalContext(), financeResolver(), service, FinanceAccountUpdateInput{Space: "Family", Account: " BCA ", Name: &name, Archived: &archived})
	if err != nil || got == nil {
		t.Fatalf("finance_account_update: %v", err)
	}
	in := service.accountUpdate
	if in.Space != financeTestSpaceID || in.ID != " BCA " || in.Name == nil || *in.Name != name || in.Archived == nil || !*in.Archived {
		t.Fatalf("input = %+v", in)
	}
}

func TestFinanceCategoryCreateMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceCategoryCreate(mcpExternalContext(), financeResolver(), service, FinanceCategoryCreateInput{Space: "Family", Name: "Makan", Kind: "expense"})
	if err != nil || got == nil || got.ID != "cat-1" {
		t.Fatalf("finance_category_create: %v, %+v", err, got)
	}
	if service.categoryCreate != (dto.CategoryCreateInput{Space: financeTestSpaceID, Name: "Makan", Kind: "expense"}) {
		t.Fatalf("input = %+v", service.categoryCreate)
	}
}

func TestFinanceCategoryListMapsInputAndEncodesEmptyAsArray(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	got, err := FinanceCategoryList(mcpExternalContext(), financeResolver(), service, FinanceCategoryListInput{Space: "Family", Kind: "income", IncludeArchived: true, Limit: 5})
	if err != nil {
		t.Fatalf("finance_category_list: %v", err)
	}
	if service.categoryList != (dto.CategoryListInput{Space: financeTestSpaceID, Kind: "income", IncludeArchived: true, Limit: 5}) {
		t.Fatalf("input = %+v", service.categoryList)
	}
	encoded, err := json.Marshal(FinanceCategoryListOutput{Categories: got})
	if err != nil || string(encoded) != `{"categories":[]}` {
		t.Fatalf("empty list encodes as %s (%v)", encoded, err)
	}
}

func TestFinanceCategoryUpdateMapsInput(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	name := "Makan Luar"
	got, err := FinanceCategoryUpdate(mcpExternalContext(), financeResolver(), service, FinanceCategoryUpdateInput{Space: "Family", Category: "Makan", Name: &name})
	if err != nil || got == nil {
		t.Fatalf("finance_category_update: %v", err)
	}
	in := service.categoryUpdate
	if in.Space != financeTestSpaceID || in.ID != "Makan" || in.Name == nil || *in.Name != name || in.Archived != nil {
		t.Fatalf("input = %+v", in)
	}
}

func TestFinanceAccountCategoryToolsMapServiceErrors(t *testing.T) {
	ctx := mcpExternalContext()
	calls := map[string]func(*mcpFinanceServiceStub) error{
		"account_create": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceAccountCreate(ctx, financeResolver(), s, FinanceAccountCreateInput{Space: "Family"})
			return err
		},
		"account_list": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceAccountList(ctx, financeResolver(), s, FinanceAccountListInput{Space: "Family"})
			return err
		},
		"account_update": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceAccountUpdate(ctx, financeResolver(), s, FinanceAccountUpdateInput{Space: "Family"})
			return err
		},
		"category_create": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceCategoryCreate(ctx, financeResolver(), s, FinanceCategoryCreateInput{Space: "Family"})
			return err
		},
		"category_list": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceCategoryList(ctx, financeResolver(), s, FinanceCategoryListInput{Space: "Family"})
			return err
		},
		"category_update": func(s *mcpFinanceServiceStub) error {
			_, err := FinanceCategoryUpdate(ctx, financeResolver(), s, FinanceCategoryUpdateInput{Space: "Family"})
			return err
		},
	}
	errs := map[string]struct {
		err  error
		code string
	}{
		"forbidden":  {serviceauthorization.ErrForbidden, "forbidden"},
		"not found":  {serviceauthorization.ErrNotFound, "not_found"},
		"validation": {&serviceauthorization.ValidationError{Field: "name", Reason: "already exists"}, "invalid_input"},
		"concurrent": {&serviceauthorization.ValidationError{Field: "id", Reason: "was changed concurrently; retry"}, "invalid_input"},
	}
	for tool, call := range calls {
		for name, tc := range errs {
			t.Run(tool+"/"+name, func(t *testing.T) {
				assertMCPCode(t, call(&mcpFinanceServiceStub{err: tc.err}), tc.code)
			})
		}
	}
}

func TestFinanceToolsRequireLinkedActor(t *testing.T) {
	service := &mcpFinanceServiceStub{}
	_, err := FinanceAccountCreate(mcpExternalContext(), &mcpExternalResolverStub{err: serviceidentity.ErrUnauthenticated}, service, FinanceAccountCreateInput{Space: "Family"})
	assertMCPCode(t, err, "unauthenticated")
	if service.calls != 0 {
		t.Fatal("unlinked actor reached finance service")
	}
}

func TestFinanceToolNamesAreRegistered(t *testing.T) {
	tools := financeTools(t, ToolServices{Finance: &mcpFinanceServiceStub{}})
	got := make([]string, 0, len(tools))
	for _, tool := range tools {
		got = append(got, tool.Name)
	}
	want := []string{"finance_account_create", "finance_account_list", "finance_account_update", "finance_category_create", "finance_category_list", "finance_category_update"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("finance tools = %v, want %v", got, want)
	}
}

func TestFinanceToolsAreSkippedWithoutService(t *testing.T) {
	if tools := financeTools(t, ToolServices{}); len(tools) != 0 {
		t.Fatalf("finance tools registered without a service: %v", tools)
	}
}

func TestFinanceToolDescriptionsCarryRules(t *testing.T) {
	for _, tool := range financeTools(t, ToolServices{Finance: &mcpFinanceServiceStub{}}) {
		for _, want := range []string{"finance Space", "list tool"} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("%s description lacks %q", tool.Name, want)
			}
		}
		if strings.HasSuffix(tool.Name, "_create") || strings.HasSuffix(tool.Name, "_update") {
			for _, want := range []string{"owner or admin", "ask an admin", "case-insensitive", "archive"} {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("%s description lacks %q", tool.Name, want)
				}
			}
		}
	}
}

type financeToolInfo struct{ Name, Description string }

// financeTools lists the finance_ tools of a real server, sorted by name.
func financeTools(t *testing.T, services ToolServices) []financeToolInfo {
	t.Helper()
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, &resolverStub{}, nil, nil, nil, nil, services)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	response := callMCPServer(t, server.URL, "profile", "secret", "tools/list", map[string]any{})
	if response.Error != nil || response.Result == nil {
		t.Fatalf("tools/list failed: %+v", response)
	}
	var tools []financeToolInfo
	seen := map[string]bool{}
	for _, tool := range response.Result.Tools {
		if !strings.HasPrefix(tool.Name, "finance_") {
			continue
		}
		if seen[tool.Name] {
			t.Fatalf("duplicate tool %s", tool.Name)
		}
		seen[tool.Name] = true
		tools = append(tools, financeToolInfo{tool.Name, tool.Description})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}
