package repositoryonboarding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"
	interfaceonboarding "family-assistant/internal/interfaces/onboarding"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfaceonboarding.RepoOnboardingInterface {
	return &Repository{DB: db}
}

var _ interfaceonboarding.RepoOnboardingInterface = (*Repository)(nil)

func (r *Repository) FindByExternalIdentity(ctx context.Context, provider, externalID string) (domainonboarding.AccountRef, error) {
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return domainonboarding.AccountRef{}, domainidentity.ErrIdentityNotFound
	}

	var account domainonboarding.AccountRef
	err := r.DB.WithContext(ctx).
		Table("external_identities ei").
		Select("ei.user_id::text, s.id::text AS space_id").
		Joins("JOIN spaces s ON s.created_by_user_id = ei.user_id AND s.type = 'PERSONAL' AND s.status = 'ACTIVE' AND s.deleted_at IS NULL").
		Where("ei.provider = ? AND ei.external_id = ? AND ei.status = 'ACTIVE' AND ei.deleted_at IS NULL", provider, externalID).
		Take(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainonboarding.AccountRef{}, domainidentity.ErrIdentityNotFound
	}
	return account, err
}

func (r *Repository) FindByPhone(ctx context.Context, phone string) (domainonboarding.AccountRef, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return domainonboarding.AccountRef{}, domainidentity.ErrIdentityNotFound
	}

	var account domainonboarding.AccountRef
	err := r.DB.WithContext(ctx).
		Table("users u").
		Select("u.id::text AS user_id, s.id::text AS space_id").
		Joins("JOIN spaces s ON s.created_by_user_id = u.id AND s.type = 'PERSONAL' AND s.status = 'ACTIVE' AND s.deleted_at IS NULL").
		Where("u.phone = ? AND u.phone_verified_at IS NOT NULL AND u.deleted_at IS NULL", phone).
		Take(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainonboarding.AccountRef{}, domainidentity.ErrIdentityNotFound
	}
	return account, err
}

func (r *Repository) LinkExternalIdentity(ctx context.Context, identity domainidentity.ExternalIdentity) error {
	identity.Provider = domainidentity.NormalizeProvider(identity.Provider)
	identity.ExternalID = strings.TrimSpace(identity.ExternalID)
	identity.UserID = strings.TrimSpace(identity.UserID)
	if identity.UserID == "" || identity.Provider == "" || identity.ExternalID == "" {
		return domainidentity.ErrIdentityNotFound
	}
	if identity.Status == "" {
		identity.Status = domainidentity.StatusActive
	}
	if identity.Metadata == nil {
		identity.Metadata = map[string]any{}
	}
	if err := r.DB.WithContext(ctx).Create(&identity).Error; err != nil {
		if isExternalIdentityDuplicate(err) {
			return domainidentity.ErrIdentityConflict
		}
		return err
	}
	return nil
}

func (r *Repository) Create(ctx context.Context, registration domainonboarding.Registration) error {
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		omit := []string{"email", "password", "password_changed_at"}
		if strings.TrimSpace(registration.User.Phone) == "" {
			omit = append(omit, "phone")
		}
		if err := tx.WithContext(ctx).Omit(omit...).Create(&registration.User).Error; err != nil {
			if isUserPhoneDuplicate(err) {
				return domainidentity.ErrPhoneAlreadyRegistered
			}
			return err
		}
		if err := tx.WithContext(ctx).Create(&registration.Space).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(&registration.Member).Error; err != nil {
			return err
		}
		identityErr := tx.WithContext(ctx).Create(&registration.Identity).Error
		if isExternalIdentityDuplicate(identityErr) {
			return domainidentity.ErrIdentityConflict
		}
		return identityErr
	})
	return err
}

func (r *Repository) FindAccountForRename(ctx context.Context, provider, externalID string) (domainonboarding.UserRef, error) {
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return domainonboarding.UserRef{}, domainidentity.ErrIdentityNotFound
	}

	var user domainonboarding.UserRef
	err := r.DB.WithContext(ctx).
		Table("external_identities ei").
		Select("u.id::text AS id, u.name AS name").
		Joins("JOIN users u ON u.id = ei.user_id").
		Where("ei.provider = ? AND ei.external_id = ? AND ei.status = 'ACTIVE' AND ei.deleted_at IS NULL AND u.deleted_at IS NULL", provider, externalID).
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainonboarding.UserRef{}, domainidentity.ErrIdentityNotFound
	}
	return user, err
}

// otherMemberNamesSQL returns the distinct display names of the other active members of every live
// Space the caller is an active member of. Names are compared in Go (service accountNameKey), never
// in SQL, so the comparison does not depend on collation or database functions.
const otherMemberNamesSQL = `SELECT DISTINCT u.name
FROM space_members mine
JOIN space_members theirs ON theirs.space_id = mine.space_id
JOIN users u ON u.id = theirs.user_id
JOIN spaces s ON s.id = mine.space_id
WHERE mine.user_id = ? AND mine.status = 'ACTIVE' AND mine.deleted_at IS NULL
  AND theirs.user_id <> mine.user_id AND theirs.status = 'ACTIVE' AND theirs.deleted_at IS NULL
  AND u.deleted_at IS NULL AND s.deleted_at IS NULL`

func (r *Repository) OtherMemberNames(ctx context.Context, userID string) ([]string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("user id is required")
	}
	var names []string
	if err := r.DB.WithContext(ctx).Raw(otherMemberNamesSQL, userID).Scan(&names).Error; err != nil {
		return nil, err
	}
	return names, nil
}

// UpdateUserName sets users.name and users.updated_at and nothing else, for exactly one live user.
func (r *Repository) UpdateUserName(ctx context.Context, userID, name string, now time.Time) error {
	userID = strings.TrimSpace(userID)
	name = strings.TrimSpace(name)
	if userID == "" || name == "" {
		return errors.New("user id and name are required")
	}
	result := r.DB.WithContext(ctx).
		Table("users").
		Where("id = ? AND deleted_at IS NULL", userID).
		Updates(map[string]any{"name": name, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("rename affected %d rows, want 1", result.RowsAffected)
	}
	return nil
}

// usersPhoneUniqueIndex is the partial unique index on users.phone.
const usersPhoneUniqueIndex = "idx_users_phone"

func isUserPhoneDuplicate(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == usersPhoneUniqueIndex
}

func isExternalIdentityDuplicate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint") || strings.Contains(message, "23505")
}
