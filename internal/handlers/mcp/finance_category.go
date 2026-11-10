package mcp

import (
	"context"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	interfaceidentity "family-assistant/internal/interfaces/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type FinanceCategoryCreateInput struct {
	Space string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	Name  string `json:"name" jsonschema:"category name, unique per Space (case-insensitive), e.g. Makan, Transport, Gaji"`
	Kind  string `json:"kind" jsonschema:"income or expense"`
}

type FinanceCategoryListInput struct {
	Space           string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	Kind            string `json:"kind,omitempty" jsonschema:"optional filter: income or expense"`
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"also list archived categories; default false"`
	Limit           int    `json:"limit,omitempty" jsonschema:"optional maximum results, default 50, capped at 100"`
}

type FinanceCategoryUpdateInput struct {
	Space    string  `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	Category string  `json:"category" jsonschema:"category UUID or exact name, from finance_category_list"`
	Name     *string `json:"name,omitempty" jsonschema:"optional new name, unique per Space (case-insensitive)"`
	Archived *bool   `json:"archived,omitempty" jsonschema:"true archives the category (hides it, keeps its transactions); false unarchives it; omit to leave unchanged"`
}

type FinanceCategoryListOutput struct {
	Categories []domainfinance.Category `json:"categories"`
}

func FinanceCategoryCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceCategoryCreateInput) (*domainfinance.Category, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*domainfinance.Category, error) {
		return service.CategoryCreate(ctx, actor, dto.CategoryCreateInput{Space: spaceID, Name: input.Name, Kind: input.Kind})
	})
}

func FinanceCategoryList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceCategoryListInput) ([]domainfinance.Category, error) {
	categories, err := financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) ([]domainfinance.Category, error) {
		return service.CategoryList(ctx, actor, dto.CategoryListInput{Space: spaceID, Kind: input.Kind, IncludeArchived: input.IncludeArchived, Limit: input.Limit})
	})
	return nonNilSlice(categories), err
}

func FinanceCategoryUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface, input FinanceCategoryUpdateInput) (*domainfinance.Category, error) {
	return financeCall(ctx, resolver, service, input.Space, func(actor domainidentity.ActorContext, spaceID string) (*domainfinance.Category, error) {
		return service.CategoryUpdate(ctx, actor, dto.CategoryUpdateInput{Space: spaceID, ID: input.Category, Name: input.Name, Archived: input.Archived})
	})
}

func registerFinanceCategoryTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacefinance.ServiceFinanceInterface) {
	addTool(server, &mcpsdk.Tool{
		Name:        "finance_category_create",
		Description: "Create an income or expense category in a finance Space (kind is income or expense)." + financeAdminRules + financeRules + " Check finance_category_list first to avoid duplicates.",
	}, func(ctx context.Context, input FinanceCategoryCreateInput) (*domainfinance.Category, error) {
		return FinanceCategoryCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name:        "finance_category_list",
		Description: "List the categories of a finance Space (id, name, kind; archived ones only with include_archived), optionally filtered by kind. This is the list tool to use for category ids and names. Any member can list." + financeRules,
	}, func(ctx context.Context, input FinanceCategoryListInput) (FinanceCategoryListOutput, error) {
		categories, err := FinanceCategoryList(ctx, resolver, service, input)
		return FinanceCategoryListOutput{Categories: categories}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name:        "finance_category_update",
		Description: "Rename or archive/unarchive a category in a finance Space. The category is its UUID or exact name; get it from finance_category_list. The kind cannot be changed." + financeAdminRules + financeRules,
	}, func(ctx context.Context, input FinanceCategoryUpdateInput) (*domainfinance.Category, error) {
		return FinanceCategoryUpdate(ctx, resolver, service, input)
	})
}
