package servicefinance

import (
	"context"
	"errors"
	"math"
	"sort"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
)

const resourceReport = "finance_report"

var errBalanceOverflow = errors.New("balance total overflows int64")

// Balances returns the balance of every non-archived account, sorted by name,
// and their sum. The repository computes balances in SQL and also returns
// archived accounts, which are filtered here. One balance is bounded by
// opening +/- MaxAmount (1e12 sen) per transaction, so the sum cannot realistically overflow;
// it is still checked so a wrapped total can never be shown as truth.
func (s *service) Balances(ctx context.Context, actor domainidentity.ActorContext, spaceArg string) (report *dto.BalancesReport, err error) {
	spaceID := resolveSpace(actor, spaceArg)
	defer func() { s.auditResult(ctx, actor, actionList, resourceReport, spaceID, "", err) }()

	if err = s.configured(); err != nil {
		return nil, err
	}
	if err = s.guard(ctx, actor, spaceID, permList); err != nil {
		return nil, err
	}
	accounts, err := s.repo.ListAccounts(ctx, spaceID, false)
	if err != nil {
		return nil, err
	}
	active := make(map[string]struct{}, len(accounts))
	for _, a := range accounts {
		active[a.ID] = struct{}{}
	}
	balances, err := s.repo.Balances(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := &dto.BalancesReport{Accounts: make([]domainfinance.AccountBalance, 0, len(balances))}
	for _, b := range balances {
		if _, ok := active[b.AccountID]; !ok {
			continue
		}
		if (b.Balance > 0 && out.Total > math.MaxInt64-b.Balance) || (b.Balance < 0 && out.Total < math.MinInt64-b.Balance) {
			return nil, errBalanceOverflow
		}
		out.Total += b.Balance
		out.Accounts = append(out.Accounts, b)
	}
	sort.Slice(out.Accounts, func(i, j int) bool {
		if out.Accounts[i].Name != out.Accounts[j].Name {
			return out.Accounts[i].Name < out.Accounts[j].Name
		}
		return out.Accounts[i].AccountID < out.Accounts[j].AccountID
	})
	return out, nil
}

// Summary reports income, expense and net for a period resolved in WIB, plus
// the immediately preceding period's totals. Totals come from the repository,
// which never counts transfers; the service adds nothing of its own.
func (s *service) Summary(ctx context.Context, actor domainidentity.ActorContext, input dto.SummaryInput) (report *dto.SummaryReport, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() { s.auditResult(ctx, actor, actionList, resourceReport, spaceID, "", err) }()

	period, err := resolvePeriod(input, s.now())
	if err != nil {
		return nil, err
	}
	if err = s.configured(); err != nil {
		return nil, err
	}
	if err = s.guard(ctx, actor, spaceID, permList); err != nil {
		return nil, err
	}
	current, err := s.repo.Summary(ctx, spaceID, period.from, period.to)
	if err != nil {
		return nil, err
	}
	previous, err := s.repo.Summary(ctx, spaceID, period.prev, period.from)
	if err != nil {
		return nil, err
	}
	report = &dto.SummaryReport{
		Period: period.name, From: period.from, To: period.to,
		Income: current.Income, Expense: current.Expense, Net: current.Income - current.Expense,
		PreviousIncome: previous.Income, PreviousExpense: previous.Expense,
		ByCategory: current.ByCategory, ByAccount: current.ByAccount, TopMerchants: current.TopMerchants,
	}
	if report.ByCategory == nil {
		report.ByCategory = []domainfinance.CategoryTotal{}
	}
	if report.ByAccount == nil {
		report.ByAccount = []domainfinance.AccountTotal{}
	}
	if report.TopMerchants == nil {
		report.TopMerchants = []domainfinance.MerchantTotal{}
	}
	return report, nil
}
