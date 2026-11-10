package repositoryfinance

import (
	"context"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/utils"
)

func (r *Repository) CreateCategory(ctx context.Context, category *domainfinance.Category) error {
	if category == nil {
		return ErrIDRequired
	}
	if err := requireSpace(category.SpaceID); err != nil {
		return err
	}
	if strings.TrimSpace(category.ID) == "" {
		category.ID = utils.CreateUUID()
	}
	return r.db(ctx).Create(category).Error
}

func (r *Repository) ListCategories(ctx context.Context, spaceID string, includeArchived bool) ([]domainfinance.Category, error) {
	if err := requireSpace(spaceID); err != nil {
		return nil, err
	}
	query := r.db(ctx).Where("space_id = ?", spaceID)
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var categories []domainfinance.Category
	if err := query.Order("kind, name, id").Find(&categories).Error; err != nil {
		return nil, err
	}
	return categories, nil
}

func (r *Repository) FindCategory(ctx context.Context, spaceID, id string) (*domainfinance.Category, error) {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return nil, err
	}
	var category domainfinance.Category
	if err := r.db(ctx).Where("space_id = ? AND id = ?", spaceID, id).First(&category).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *Repository) UpdateCategory(ctx context.Context, category *domainfinance.Category) error {
	if category == nil {
		return ErrIDRequired
	}
	if err := requireSpaceAndID(category.SpaceID, category.ID); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Category{}, category.SpaceID, category.ID, map[string]any{
		"name": category.Name, "kind": category.Kind, "updated_at": time.Now().UTC(),
	})
}

func (r *Repository) ArchiveCategory(ctx context.Context, spaceID, id string, at time.Time) error {
	if err := requireSpaceAndID(spaceID, id); err != nil {
		return err
	}
	return updateScoped(r.db(ctx), &domainfinance.Category{}, spaceID, id, stamp("archived_at", at))
}
