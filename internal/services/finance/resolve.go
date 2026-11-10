package servicefinance

import (
	"context"
	"errors"
	"strings"

	domainfinance "family-assistant/internal/domain/finance"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	reasonNotFound = "not found"
	reasonArchived = "is archived"

	fieldAccount   = "account"
	fieldToAccount = "to_account"
	fieldCategory  = "category"
)

// refRow is what resolution needs from an account or category row.
type refRow struct {
	id       string
	archived bool
	value    any // the loaded row, for callers that need more than the id
}

// refLookup reads one resource kind, always scoped to the caller's Space.
type refLookup struct {
	byID   func(id string) (refRow, error)
	byName func(name string) ([]refRow, error)
}

// resolveRef turns a UUID or an exact case-insensitive name into a row. An
// unknown reference, one from another Space and a soft-deleted row all report
// "not found" so a foreign id cannot be told apart from a missing one. An
// archived row is accepted only with allowArchived (list filters, unchanged
// references of an edited transaction).
func resolveRef(field, ref string, allowArchived bool, lookup refLookup) (refRow, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return refRow{}, validationError(field, requiredReason)
	}
	var row refRow
	if _, err := uuid.Parse(ref); err == nil {
		found, err := lookup.byID(ref)
		if err != nil {
			return refRow{}, refError(field, err)
		}
		row = found
	} else {
		rows, err := lookup.byName(ref)
		if err != nil {
			return refRow{}, refError(field, err)
		}
		picked, ok := pickRow(rows)
		if !ok {
			return refRow{}, validationError(field, reasonNotFound)
		}
		row = picked
	}
	if row.archived && !allowArchived {
		return refRow{}, validationError(field, reasonArchived)
	}
	return row, nil
}

// pickRow prefers a live row over an archived one with the same name.
func pickRow(rows []refRow) (refRow, bool) {
	if len(rows) == 0 {
		return refRow{}, false
	}
	for _, row := range rows {
		if !row.archived {
			return row, true
		}
	}
	return rows[0], true
}

func refError(field string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return validationError(field, reasonNotFound)
	}
	return err
}

func (s *service) resolveAccount(ctx context.Context, spaceID, ref, field string, allowArchived bool) (string, error) {
	row, err := resolveRef(field, ref, allowArchived, refLookup{
		byID: func(id string) (refRow, error) {
			a, err := s.repo.FindAccount(ctx, spaceID, id)
			if err != nil {
				return refRow{}, err
			}
			return refRow{id: a.ID, archived: a.ArchivedAt != nil}, nil
		},
		byName: func(name string) ([]refRow, error) {
			accounts, err := s.repo.ListAccounts(ctx, spaceID, true)
			if err != nil {
				return nil, err
			}
			var rows []refRow
			for _, a := range accounts {
				if strings.EqualFold(strings.TrimSpace(a.Name), name) {
					rows = append(rows, refRow{id: a.ID, archived: a.ArchivedAt != nil})
				}
			}
			return rows, nil
		},
	})
	return row.id, err
}

// resolveCategory resolves a category and returns its row. The default
// categories are seeded first so a name like "Makan" works on first use.
func (s *service) resolveCategory(ctx context.Context, spaceID, ref string, allowArchived bool) (*domainfinance.Category, error) {
	if err := s.ensureDefaultCategories(ctx, spaceID); err != nil {
		return nil, err
	}
	row, err := resolveRef(fieldCategory, ref, allowArchived, refLookup{
		byID: func(id string) (refRow, error) {
			c, err := s.repo.FindCategory(ctx, spaceID, id)
			if err != nil {
				return refRow{}, err
			}
			return refRow{id: c.ID, archived: c.ArchivedAt != nil, value: c}, nil
		},
		byName: func(name string) ([]refRow, error) {
			categories, err := s.repo.ListCategories(ctx, spaceID, true)
			if err != nil {
				return nil, err
			}
			var rows []refRow
			for i := range categories {
				if strings.EqualFold(strings.TrimSpace(categories[i].Name), name) {
					rows = append(rows, refRow{id: categories[i].ID, archived: categories[i].ArchivedAt != nil, value: &categories[i]})
				}
			}
			return rows, nil
		},
	})
	if err != nil {
		return nil, err
	}
	category, _ := row.value.(*domainfinance.Category)
	return category, nil
}
