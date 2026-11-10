package servicefinance

import (
	"context"
	"strings"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	"family-assistant/utils"
)

const accountTypeCreditCard = "credit_card"

var accountTypes = map[string]struct{}{
	"cash": {}, "bank": {}, "ewallet": {}, "savings": {}, accountTypeCreditCard: {},
}

func (s *service) AccountCreate(ctx context.Context, actor domainidentity.ActorContext, input dto.AccountCreateInput) (created *domainfinance.Account, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() {
		resourceID := ""
		if created != nil {
			resourceID = created.ID
		}
		s.auditResult(ctx, actor, domainaudit.ActionCreate, resourceAccount, spaceID, resourceID, err)
	}()

	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, err
	}
	accountType := strings.ToLower(strings.TrimSpace(input.Type))
	if _, ok := accountTypes[accountType]; !ok {
		return nil, validationError("type", "must be cash, bank, ewallet, savings or credit_card")
	}
	if input.OpeningBalance > domainfinance.MaxAmount || input.OpeningBalance < -domainfinance.MaxAmount {
		return nil, validationError("opening_balance", "is out of range")
	}
	if input.OpeningBalance < 0 && accountType != accountTypeCreditCard {
		return nil, validationError("opening_balance", "may be negative only for credit_card accounts")
	}
	if err := s.configured(); err != nil {
		return nil, err
	}
	if err := s.guard(ctx, actor, spaceID, permManage); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	account := &domainfinance.Account{
		ID: utils.CreateUUID(), SpaceID: spaceID, Name: name, Type: accountType, OpeningBalance: input.OpeningBalance,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateAccount(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *service) AccountList(ctx context.Context, actor domainidentity.ActorContext, input dto.AccountListInput) (accounts []domainfinance.Account, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() { s.auditResult(ctx, actor, actionList, resourceAccount, spaceID, "", err) }()

	if err := s.configured(); err != nil {
		return nil, err
	}
	limit, err := clampLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	if err := s.guard(ctx, actor, spaceID, permList); err != nil {
		return nil, err
	}
	accounts, err = s.repo.ListAccounts(ctx, spaceID, input.IncludeArchived)
	if err != nil {
		return nil, err
	}
	if len(accounts) > limit {
		accounts = accounts[:limit]
	}
	return accounts, nil
}

// AccountUpdate renames and/or archives an account. Archiving only sets
// archived_at; the account and its transactions are never deleted.
func (s *service) AccountUpdate(ctx context.Context, actor domainidentity.ActorContext, input dto.AccountUpdateInput) (*domainfinance.Account, error) {
	spaceID := resolveSpace(actor, input.Space)
	var account *domainfinance.Account
	load := func(id string) (err error) { account, err = s.repo.FindAccount(ctx, spaceID, id); return err }
	err := s.updateRow(ctx, actor, spaceID, resourceAccount, input.ID, input.Name, input.Archived, rowOps{
		load:    load,
		current: func() (string, bool) { return account.Name, account.ArchivedAt != nil },
		rename: func(name string) error {
			account.Name = name
			return s.repo.UpdateAccount(ctx, account)
		},
		archive: func(id string, at time.Time) error { return s.repo.ArchiveAccount(ctx, spaceID, id, at) },
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}
