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
	reasonNotFound  = "not found"
	reasonArchived  = "is archived"
	reasonAmbiguous = "is ambiguous"

	fieldAccount   = "account"
	fieldToAccount = "to_account"
	fieldCategory  = "category"
)

var errKindMismatch = errors.New("category kind does not match")

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
// archived row is accepted only with allowArchived (list filters) or when its
// id is current (the reference an edited transaction already holds). A name
// that still matches more than one row is "is ambiguous".
func resolveRef(field, ref string, allowArchived bool, current string, lookup refLookup) (refRow, error) {
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
		switch len(rows) {
		case 0:
			return refRow{}, validationError(field, reasonNotFound)
		case 1:
			row = rows[0]
		default:
			return refRow{}, validationError(field, reasonAmbiguous)
		}
	}
	if row.archived && !allowArchived && row.id != current {
		return refRow{}, validationError(field, reasonArchived)
	}
	return row, nil
}

func refError(field string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return validationError(field, reasonNotFound)
	}
	return err
}

func (s *service) resolveAccount(ctx context.Context, spaceID, ref, field string, allowArchived bool, current string) (string, error) {
	row, err := resolveRef(field, ref, allowArchived, current, refLookup{
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
// categories are seeded first so a name like "Makan" works on first use. The
// name index is unique per kind, so one name can exist as both income and
// expense: kind (income or expense, "" for none) narrows a name match, and a
// name that still matches both kinds is ambiguous. An id ignores kind.
func (s *service) resolveCategory(ctx context.Context, spaceID, ref, kind string, allowArchived bool, current string) (*domainfinance.Category, error) {
	if err := s.ensureDefaultCategories(ctx, spaceID); err != nil {
		return nil, err
	}
	row, err := resolveRef(fieldCategory, ref, allowArchived, current, refLookup{
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
			otherKind := false
			for i := range categories {
				if !strings.EqualFold(strings.TrimSpace(categories[i].Name), name) {
					continue
				}
				if kind != "" && categories[i].Kind != kind {
					otherKind = true
					continue
				}
				rows = append(rows, refRow{id: categories[i].ID, archived: categories[i].ArchivedAt != nil, value: &categories[i]})
			}
			if len(rows) == 0 && otherKind {
				return nil, errKindMismatch
			}
			return rows, nil
		},
	})
	if errors.Is(err, errKindMismatch) {
		return nil, validationError(fieldCategory, "kind does not match type")
	}
	if err != nil {
		return nil, err
	}
	category, _ := row.value.(*domainfinance.Category)
	return category, nil
}
