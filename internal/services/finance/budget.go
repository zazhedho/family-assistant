package servicefinance

import (
	"context"
	"sort"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
)

const resourceBudget = "finance_budget"

// budgetCategory resolves the expense category a budget belongs to. A budget
// exists only for expense categories, whether the ref is a name or an id.
func (s *service) budgetCategory(ctx context.Context, spaceID, ref string, allowArchived bool) (*domainfinance.Category, error) {
	category, err := s.resolveCategory(ctx, spaceID, ref, domainfinance.TypeExpense, allowArchived, "")
	if err != nil {
		return nil, err
	}
	if category.Kind != domainfinance.TypeExpense {
		return nil, validationError(fieldCategory, "kind does not match type")
	}
	return category, nil
}

// BudgetSet sets (inserts or overwrites) the budget of an expense category for
// a WIB month.
func (s *service) BudgetSet(ctx context.Context, actor domainidentity.ActorContext, input dto.BudgetSetInput) (budget *domainfinance.Budget, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() {
		resourceID := ""
		if budget != nil {
			resourceID = budget.ID
		}
		s.auditResult(ctx, actor, domainaudit.ActionUpdate, resourceBudget, spaceID, resourceID, err)
	}()

	month, err := parseBudgetMonth(input.Month, s.now())
	if err != nil {
		return nil, err
	}
	if err = mapDomainError(domainfinance.ValidateAmount(input.Amount)); err != nil {
		return nil, err
	}
	if err = s.configured(); err != nil {
		return nil, err
	}
	if err = s.guard(ctx, actor, spaceID, permManage); err != nil {
		return nil, err
	}
	category, err := s.budgetCategory(ctx, spaceID, input.Category, false)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	stored := &domainfinance.Budget{
		ID: utils.CreateUUID(), SpaceID: spaceID, CategoryID: category.ID, Month: domainfinance.MonthDate(month),
		Amount: input.Amount, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.repo.UpsertBudget(ctx, stored); err != nil {
		return nil, err
	}
	return stored, nil
}

// BudgetDelete soft-deletes the budget of a category for a month. A budget that
// does not exist is ErrNotFound.
func (s *service) BudgetDelete(ctx context.Context, actor domainidentity.ActorContext, input dto.BudgetDeleteInput) (err error) {
	spaceID := resolveSpace(actor, input.Space)
	budgetID := ""
	defer func() { s.auditResult(ctx, actor, domainaudit.ActionDelete, resourceBudget, spaceID, budgetID, err) }()

	month, err := parseBudgetMonth(input.Month, s.now())
	if err != nil {
		return err
	}
	if err = s.configured(); err != nil {
		return err
	}
	if err = s.guard(ctx, actor, spaceID, permManage); err != nil {
		return err
	}
	category, err := s.budgetCategory(ctx, spaceID, input.Category, true)
	if err != nil {
		return err
	}
	budgets, err := s.repo.ListBudgets(ctx, spaceID, domainfinance.MonthDate(month))
	if err != nil {
		return err
	}
	for _, b := range budgets {
		if b.CategoryID == category.ID {
			budgetID = b.ID
			return notFound(s.repo.DeleteBudget(ctx, spaceID, b.ID, s.now().UTC()))
		}
	}
	return serviceauthorization.ErrNotFound
}

// BudgetList reports spent, remaining and percent used for every budget of a
// WIB month ("" is the current one), sorted by category name. Spending comes
// from the repository over the half-open WIB month; a failure fails the call.
func (s *service) BudgetList(ctx context.Context, actor domainidentity.ActorContext, spaceArg, monthArg string) (views []dto.BudgetStatusView, err error) {
	spaceID := resolveSpace(actor, spaceArg)
	defer func() { s.auditResult(ctx, actor, actionList, resourceBudget, spaceID, "", err) }()

	month := s.now()
	if monthArg != "" {
		if month, err = parseBudgetMonth(monthArg, month); err != nil {
			return nil, err
		}
	}
	if err = s.configured(); err != nil {
		return nil, err
	}
	if err = s.guard(ctx, actor, spaceID, permList); err != nil {
		return nil, err
	}
	budgets, err := s.repo.ListBudgets(ctx, spaceID, domainfinance.MonthDate(month))
	if err != nil {
		return nil, err
	}
	categories, err := s.repo.ListCategories(ctx, spaceID, true)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(categories))
	for _, c := range categories {
		names[c.ID] = c.Name
	}
	from, to := monthRange(month)
	views = make([]dto.BudgetStatusView, 0, len(budgets))
	for _, b := range budgets {
		spent, err := s.repo.SpentByCategory(ctx, spaceID, b.CategoryID, from, to)
		if err != nil {
			return nil, err
		}
		views = append(views, dto.BudgetStatusView{
			BudgetStatus: domainfinance.NewBudgetStatus(b.CategoryID, b.Amount, spent), CategoryName: names[b.CategoryID],
		})
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].CategoryName != views[j].CategoryName {
			return views[i].CategoryName < views[j].CategoryName
		}
		return views[i].CategoryID < views[j].CategoryID
	})
	return views, nil
}
