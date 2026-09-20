package repositoryinvitation

import (
	"context"
	"errors"
	"strings"
	"time"

	domaininvitation "family-assistant/internal/domain/invitation"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{DB: db}
}

var _ domaininvitation.Repository = (*Repository)(nil)

func (r *Repository) Create(ctx context.Context, invitation *domaininvitation.Invitation) error {
	if invitation == nil {
		return errors.New("invitation is required")
	}
	if strings.TrimSpace(invitation.ID) == "" {
		invitation.ID = utils.CreateUUID()
	}
	if strings.TrimSpace(invitation.Status) == "" {
		invitation.Status = domaininvitation.StatusPending
	}
	return r.DB.WithContext(ctx).Create(invitation).Error
}

func (r *Repository) Accept(ctx context.Context, tokenHash, userID, normalizedEmail string, now time.Time) (*domaininvitation.Acceptance, error) {
	if strings.TrimSpace(tokenHash) == "" || strings.TrimSpace(userID) == "" {
		return nil, domaininvitation.ErrInvalidInvitation
	}

	var acceptance *domaininvitation.Acceptance
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var invitation domaininvitation.Invitation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ? AND deleted_at IS NULL", tokenHash).
			Take(&invitation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domaininvitation.ErrInvalidInvitation
		}
		if err != nil {
			return err
		}
		if invitation.Status != domaininvitation.StatusPending || invitation.AcceptedAt != nil ||
			strings.TrimSpace(invitation.AcceptedByUserID) != "" || !now.Before(invitation.ExpiresAt) {
			return domaininvitation.ErrInvalidInvitation
		}
		if strings.TrimSpace(invitation.InvitedEmail) != "" &&
			!strings.EqualFold(strings.TrimSpace(invitation.InvitedEmail), strings.TrimSpace(normalizedEmail)) {
			return domaininvitation.ErrInvalidInvitation
		}

		var space domainspace.Space
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND type = ? AND status = ? AND deleted_at IS NULL", invitation.SpaceID, domainspace.TypeShared, domainspace.StatusActive).
			Take(&space).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domaininvitation.ErrInvalidInvitation
		}
		if err != nil {
			return err
		}
		if space.Type != domainspace.TypeShared || space.Status != domainspace.StatusActive || space.DeletedAt.Valid {
			return domaininvitation.ErrInvalidInvitation
		}

		member := &domainspace.Member{
			ID:        utils.CreateUUID(),
			SpaceID:   invitation.SpaceID,
			UserID:    strings.TrimSpace(userID),
			RoleID:    invitation.RoleID,
			Status:    domainspace.StatusActive,
			CreatedAt: now,
		}
		if err := tx.WithContext(ctx).Create(member).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return domaininvitation.ErrMembershipConflict
			}
			return err
		}

		if err := tx.Model(&invitation).Updates(map[string]any{
			"status":              domaininvitation.StatusAccepted,
			"accepted_at":         now,
			"accepted_by_user_id": strings.TrimSpace(userID),
			"updated_at":          now,
		}).Error; err != nil {
			return err
		}
		safeInvitation := invitation
		safeInvitation.TokenHash = ""
		acceptance = &domaininvitation.Acceptance{
			InvitationID: invitation.ID,
			SpaceID:      invitation.SpaceID,
			RoleID:       invitation.RoleID,
			RoleName:     invitation.RoleID,
			EmailBound:   strings.TrimSpace(invitation.InvitedEmail) != "",
			Invitation:   &safeInvitation,
			Member:       member,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return acceptance, nil
}
