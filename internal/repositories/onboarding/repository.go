package repositoryonboarding

import (
	"context"
	"errors"
	"strings"

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
		Where("u.phone = ? AND u.deleted_at IS NULL", phone).
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
