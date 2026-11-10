package servicefinance

import (
	"errors"
	"strings"
	"unicode/utf8"

	domainfinance "family-assistant/internal/domain/finance"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// mapDomainError converts a domain validation sentinel to a field-level
// ValidationError. Unknown errors, and nil, pass through unchanged.
func mapDomainError(err error) error {
	field := ""
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domainfinance.ErrInvalidType):
		field = "type"
	case errors.Is(err, domainfinance.ErrInvalidAmount):
		field = "amount"
	case errors.Is(err, domainfinance.ErrAccountRequired):
		field = "account_id"
	case errors.Is(err, domainfinance.ErrToAccountRequired),
		errors.Is(err, domainfinance.ErrToAccountForbidden),
		errors.Is(err, domainfinance.ErrTransferSameAcct):
		field = "to_account_id"
	case errors.Is(err, domainfinance.ErrTransferCategory):
		field = "category_id"
	default:
		return err
	}
	return &serviceauthorization.ValidationError{Field: field, Reason: err.Error()}
}

func validationError(field, reason string) error {
	return &serviceauthorization.ValidationError{Field: field, Reason: reason}
}

// normalizeName trims name and checks it against the column limits.
func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", validationError("name", requiredReason)
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return "", validationError("name", "is too long")
	}
	return name, nil
}

// requireID trims id and checks that it is a UUID.
func requireID(id string) (string, error) {
	parsed, err := utils.ParseUUID(id, "id", true)
	if err != nil {
		return "", validationError("id", utils.ErrInvalidUUID.Error())
	}
	return parsed, nil
}

// notFound maps a missing row to ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return serviceauthorization.ErrNotFound
	}
	return err
}

// validateUpdate checks the fields shared by account and category updates and
// returns the trimmed id and new name (empty when Name is nil).
func validateUpdate(rawID string, name *string, archived *bool) (id, newName string, err error) {
	if id, err = requireID(rawID); err != nil {
		return "", "", err
	}
	if name == nil && archived == nil {
		return "", "", validationError("update", "at least one field is required")
	}
	if name != nil {
		if newName, err = normalizeName(*name); err != nil {
			return "", "", err
		}
	}
	return id, newName, nil
}

// archiveChange is what an update does to archived_at.
type archiveChange int

const (
	archiveNoop archiveChange = iota
	archiveSet
	archiveClear
)

// wantArchive turns the tri-state Archived flag into an action. Both
// directions are idempotent: archiving an archived row or unarchiving a live
// one is a no-op, so archived_at is never re-stamped.
func wantArchive(archived *bool, currentlyArchived bool) archiveChange {
	switch {
	case archived == nil, *archived == currentlyArchived:
		return archiveNoop
	case *archived:
		return archiveSet
	default:
		return archiveClear
	}
}

// mapUniqueViolation maps a Postgres unique violation (23505) to a name
// ValidationError; other errors pass through.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return validationError("name", "already exists")
	}
	return err
}
