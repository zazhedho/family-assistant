package repositoryidentity

import (
	"context"
	"errors"
	"strings"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	"family-assistant/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfaceidentity.RepoIdentityInterface {
	return &Repository{DB: db}
}

var _ interfaceidentity.RepoIdentityInterface = (*Repository)(nil)

func (r *Repository) CreateLinkToken(ctx context.Context, token *domainidentity.LinkToken) error {
	if token == nil {
		return errors.New("link token is required")
	}
	if strings.TrimSpace(token.ID) == "" {
		token.ID = utils.CreateUUID()
	}
	token.UserID = strings.TrimSpace(token.UserID)
	token.Provider = domainidentity.NormalizeProvider(token.Provider)
	token.TokenHash = strings.TrimSpace(token.TokenHash)
	if token.UserID == "" || token.Provider == "" || token.TokenHash == "" {
		return domainidentity.ErrInvalidLinkToken
	}
	if token.UpdatedAt.IsZero() {
		token.UpdatedAt = token.CreatedAt
	}
	return r.DB.WithContext(ctx).Create(token).Error
}

func (r *Repository) ConsumeAndLink(ctx context.Context, tokenHash, provider, externalID string, now time.Time) (*domainidentity.ExternalIdentity, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if tokenHash == "" || provider == "" || externalID == "" {
		return nil, domainidentity.ErrInvalidLinkToken
	}

	var identity *domainidentity.ExternalIdentity
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token domainidentity.LinkToken
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ? AND deleted_at IS NULL", tokenHash).
			Take(&token).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainidentity.ErrInvalidLinkToken
		}
		if err != nil {
			return err
		}
		if domainidentity.NormalizeProvider(token.Provider) != provider || token.ConsumedAt != nil || !now.Before(token.ExpiresAt) {
			return domainidentity.ErrInvalidLinkToken
		}
		if strings.TrimSpace(token.UserID) == "" {
			return domainidentity.ErrInvalidLinkToken
		}

		created := &domainidentity.ExternalIdentity{
			ID:         utils.CreateUUID(),
			UserID:     strings.TrimSpace(token.UserID),
			Provider:   provider,
			ExternalID: externalID,
			Status:     domainidentity.StatusActive,
			VerifiedAt: now,
			Metadata:   map[string]any{},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := tx.Create(created).Error; err != nil {
			if isDuplicateError(err) {
				return domainidentity.ErrIdentityConflict
			}
			return err
		}
		result := tx.Model(&token).Updates(map[string]any{
			"consumed_at": now,
			"updated_at":  now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return domainidentity.ErrInvalidLinkToken
		}
		identity = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return identity, nil
}

func isDuplicateError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint") || strings.Contains(message, "23505")
}

func (r *Repository) FindActive(ctx context.Context, provider, externalID string) (*domainidentity.ExternalIdentity, error) {
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return nil, domainidentity.ErrIdentityNotFound
	}
	var identity domainidentity.ExternalIdentity
	err := r.DB.WithContext(ctx).
		Where("provider = ? AND external_id = ? AND status = ? AND deleted_at IS NULL", provider, externalID, domainidentity.StatusActive).
		Take(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainidentity.ErrIdentityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func (r *Repository) Revoke(ctx context.Context, userID, provider, externalID string) error {
	userID = strings.TrimSpace(userID)
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if userID == "" || provider == "" || externalID == "" {
		return domainidentity.ErrIdentityNotFound
	}
	result := r.DB.WithContext(ctx).Model(&domainidentity.ExternalIdentity{}).
		Where("user_id = ? AND provider = ? AND external_id = ? AND status = ? AND deleted_at IS NULL", userID, provider, externalID, domainidentity.StatusActive).
		Updates(map[string]any{"status": domainidentity.StatusRevoked, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainidentity.ErrIdentityNotFound
	}
	return nil
}
