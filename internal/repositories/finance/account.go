package repositoryfinance

import (
	"context"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/utils"
)

func (r *Repository) CreateAccount(ctx context.Context, account *domainfinance.Account) error {
	if account == nil {
		return ErrIDRequired
	}
	if err := requireSpace(account.SpaceID); err != nil {
		return err
	}
	if strings.TrimSpace(account.ID) == "" {
		account.ID = utils.CreateUUID()
	}
	return r.db(ctx).Create(account).Error
}

func (r *Repository) ListAccounts(ctx context.Context, spaceID string, includeArchived bool) ([]domainfinance.Account, error) {
	if err := requireSpace(spaceID); err != nil {
		return nil, err
	}
	query := r.db(ctx).Where("space_id = ?", spaceID)
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var accounts []domainfinance.Account
	if err := query.Order("created_at, id").Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *Repository) FindAccount(ctx context.Context, spaceID, id string) (*domainfinance.Account, error) {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return nil, err
	}
	var account domainfinance.Account
	if err := r.db(ctx).Where("space_id = ? AND id = ?", spaceID, id).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *Repository) UpdateAccount(ctx context.Context, account *domainfinance.Account) error {
	if account == nil {
		return ErrIDRequired
	}
	if err := requireSpaceAndID(account.SpaceID, account.ID); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Account{}, account.SpaceID, account.ID, map[string]any{
		"name": account.Name, "type": account.Type, "opening_balance": account.OpeningBalance, "updated_at": time.Now().UTC(),
	})
}

func (r *Repository) ArchiveAccount(ctx context.Context, spaceID, id string, at time.Time) error {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Account{}, spaceID, id, stamp("archived_at", at))
}

func (r *Repository) UnarchiveAccount(ctx context.Context, spaceID, id string) error {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Account{}, spaceID, id, clearArchive(time.Now().UTC()))
}
