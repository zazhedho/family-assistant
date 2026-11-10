package servicefinance

import (
	"context"
	"strings"
	"sync"
	"time"

	domainauthorization "family-assistant/internal/domain/authorization"
	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
)

// guard authorizes the permission, then requires the actor to be an active
// member of spaceID and the Space to be a finance Space. A non-member and a
// non-finance Space return the same ErrNotFound as a missing Space.
func (s *service) guard(ctx context.Context, actor domainidentity.ActorContext, spaceID, permission string) error {
	if err := s.authorize.Authorize(ctx, actor, permission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return err
	}
	memberID := strings.TrimSpace(actor.MemberID)
	userID := strings.TrimSpace(actor.UserID)
	for _, member := range members {
		if strings.TrimSpace(member.Status) != domainspace.StatusActive || strings.TrimSpace(member.SpaceID) != spaceID {
			continue
		}
		if (memberID != "" && strings.TrimSpace(member.ID) == memberID) || (memberID == "" && strings.TrimSpace(member.UserID) == userID) {
			if strings.TrimSpace(member.SpaceCategory) != domainspace.CategoryFinance {
				return serviceauthorization.ErrNotFound
			}
			return nil
		}
	}
	return serviceauthorization.ErrNotFound
}

type defaultCategory struct{ name, kind string }

var defaultCategories = []defaultCategory{
	{"Makan", domainfinance.TypeExpense}, {"Transport", domainfinance.TypeExpense}, {"Belanja", domainfinance.TypeExpense},
	{"Tagihan", domainfinance.TypeExpense}, {"Kesehatan", domainfinance.TypeExpense}, {"Anak", domainfinance.TypeExpense},
	{"Hiburan", domainfinance.TypeExpense}, {"Lainnya", domainfinance.TypeExpense},
	{"Gaji", domainfinance.TypeIncome}, {"Bonus", domainfinance.TypeIncome}, {"Lainnya", domainfinance.TypeIncome},
}

// ensureDefaultCategories seeds the default categories when the Space has no
// category at all (archived ones count, so archiving every default never
// re-seeds). It is idempotent. The migration has no unique index on category
// names, so concurrent first calls are serialized per Space with an in-process
// lock around check-then-create; this does not cover several app instances.
func (s *service) ensureDefaultCategories(ctx context.Context, spaceID string) error {
	lock, _ := s.seedLocks.LoadOrStore(spaceID, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	existing, err := s.repo.ListCategories(ctx, spaceID, true)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, def := range defaultCategories {
		category := &domainfinance.Category{
			ID: utils.CreateUUID(), SpaceID: spaceID, Name: def.name, Kind: def.kind, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.CreateCategory(ctx, category); err != nil {
			return err
		}
	}
	return nil
}
