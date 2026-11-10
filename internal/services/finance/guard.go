package servicefinance

import (
	"context"
	"strings"
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
	_, err := s.guardMember(ctx, actor, spaceID, permission)
	return err
}

// guardMember is guard that also returns the id of the actor's active member
// row, which is what created_by_member_id must hold.
func (s *service) guardMember(ctx context.Context, actor domainidentity.ActorContext, spaceID, permission string) (string, error) {
	if err := s.authorize.Authorize(ctx, actor, permission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return "", err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return "", err
	}
	memberID := strings.TrimSpace(actor.MemberID)
	userID := strings.TrimSpace(actor.UserID)
	for _, member := range members {
		if strings.TrimSpace(member.Status) != domainspace.StatusActive || strings.TrimSpace(member.SpaceID) != spaceID {
			continue
		}
		if (memberID != "" && strings.TrimSpace(member.ID) == memberID) || (memberID == "" && strings.TrimSpace(member.UserID) == userID) {
			if strings.TrimSpace(member.SpaceCategory) != domainspace.CategoryFinance {
				return "", serviceauthorization.ErrNotFound
			}
			return strings.TrimSpace(member.ID), nil
		}
	}
	return "", serviceauthorization.ErrNotFound
}

type defaultCategory struct{ name, kind string }

var defaultCategories = []defaultCategory{
	{"Makan", domainfinance.TypeExpense}, {"Transport", domainfinance.TypeExpense}, {"Belanja", domainfinance.TypeExpense},
	{"Tagihan", domainfinance.TypeExpense}, {"Kesehatan", domainfinance.TypeExpense}, {"Anak", domainfinance.TypeExpense},
	{"Hiburan", domainfinance.TypeExpense}, {"Lainnya", domainfinance.TypeExpense},
	{"Gaji", domainfinance.TypeIncome}, {"Bonus", domainfinance.TypeIncome}, {"Lainnya", domainfinance.TypeIncome},
}

// ensureDefaultCategories seeds the default categories through the repository,
// which does it in one transaction under a per-Space advisory lock and only
// when the Space has no category row of any state. It is idempotent and safe
// for concurrent first calls, also across app instances.
func (s *service) ensureDefaultCategories(ctx context.Context, spaceID string) error {
	defaults := make([]domainfinance.Category, len(defaultCategories))
	now := time.Now().UTC()
	for i, def := range defaultCategories {
		defaults[i] = domainfinance.Category{ID: utils.CreateUUID(), SpaceID: spaceID, Name: def.name, Kind: def.kind, CreatedAt: now, UpdatedAt: now}
	}
	return s.repo.SeedDefaultCategories(ctx, spaceID, defaults)
}
