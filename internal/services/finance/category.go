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

func normalizeKind(kind string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != domainfinance.TypeIncome && kind != domainfinance.TypeExpense {
		return "", validationError("kind", "must be income or expense")
	}
	return kind, nil
}

func (s *service) CategoryCreate(ctx context.Context, actor domainidentity.ActorContext, input dto.CategoryCreateInput) (created *domainfinance.Category, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() {
		resourceID := ""
		if created != nil {
			resourceID = created.ID
		}
		s.auditResult(ctx, actor, domainaudit.ActionCreate, resourceCategory, spaceID, resourceID, err)
	}()

	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, err
	}
	kind, err := normalizeKind(input.Kind)
	if err != nil {
		return nil, err
	}
	if err := s.configured(); err != nil {
		return nil, err
	}
	if err := s.guard(ctx, actor, spaceID, permManage); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultCategories(ctx, spaceID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	category := &domainfinance.Category{ID: utils.CreateUUID(), SpaceID: spaceID, Name: name, Kind: kind, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateCategory(ctx, category); err != nil {
		return nil, mapUniqueViolation(err)
	}
	return category, nil
}

func (s *service) CategoryList(ctx context.Context, actor domainidentity.ActorContext, input dto.CategoryListInput) (categories []domainfinance.Category, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() { s.auditResult(ctx, actor, actionList, resourceCategory, spaceID, "", err) }()

	if err := s.configured(); err != nil {
		return nil, err
	}
	limit, err := clampLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	kind := ""
	if strings.TrimSpace(input.Kind) != "" {
		if kind, err = normalizeKind(input.Kind); err != nil {
			return nil, err
		}
	}
	if err := s.guard(ctx, actor, spaceID, permList); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultCategories(ctx, spaceID); err != nil {
		return nil, err
	}
	all, err := s.repo.ListCategories(ctx, spaceID, input.IncludeArchived)
	if err != nil {
		return nil, err
	}
	categories = make([]domainfinance.Category, 0, len(all))
	for _, category := range all {
		if kind == "" || category.Kind == kind {
			categories = append(categories, category)
		}
	}
	if len(categories) > limit {
		categories = categories[:limit]
	}
	return categories, nil
}

// categoryOps wires updateRow to the category repository methods. The loaded
// row is stored in *row.
func (s *service) categoryOps(ctx context.Context, spaceID string, row **domainfinance.Category) rowOps {
	return rowOps{
		resolve: func(ref string) (string, error) {
			category, err := s.resolveCategory(ctx, spaceID, ref, "", true, "")
			if err != nil {
				return "", err
			}
			return category.ID, nil
		},
		load: func(id string) (err error) {
			*row, err = s.repo.FindCategory(ctx, spaceID, id)
			return err
		},
		current: func() (string, bool) { return (*row).Name, (*row).ArchivedAt != nil },
		rename: func(name string) error {
			(*row).Name = name
			return s.repo.UpdateCategory(ctx, *row)
		},
		archive:   func(id string, at time.Time) error { return s.repo.ArchiveCategory(ctx, spaceID, id, at) },
		unarchive: func(id string) error { return s.repo.UnarchiveCategory(ctx, spaceID, id) },
	}
}

// CategoryUpdate renames, archives or unarchives a category; it never deletes one.
func (s *service) CategoryUpdate(ctx context.Context, actor domainidentity.ActorContext, input dto.CategoryUpdateInput) (*domainfinance.Category, error) {
	spaceID := resolveSpace(actor, input.Space)
	var category *domainfinance.Category
	ops := s.categoryOps(ctx, spaceID, &category)
	if err := s.updateRow(ctx, actor, spaceID, resourceCategory, input.ID, input.Name, input.Archived, ops); err != nil {
		return nil, err
	}
	return category, nil
}
