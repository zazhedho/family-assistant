package repositoryinvitation

import (
	"context"
	"errors"
	"strings"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainspace "family-assistant/internal/domain/space"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	"family-assistant/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfaceinvitation.RepoInvitationInterface {
	return &Repository{DB: db}
}

var _ interfaceinvitation.RepoInvitationInterface = (*Repository)(nil)

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
			Select("space_invitations.*, roles.name AS role_name").
			Joins("JOIN roles ON roles.id = space_invitations.role_id AND roles.deleted_at IS NULL").
			Where("space_invitations.token_hash = ? AND space_invitations.deleted_at IS NULL", tokenHash).
			Take(&invitation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domaininvitation.ErrInvalidInvitation
		}
		if err != nil {
			return err
		}
		if invitation.Status != domaininvitation.StatusPending || invitation.AcceptedAt != nil ||
			(invitation.AcceptedByUserID != nil && strings.TrimSpace(*invitation.AcceptedByUserID) != "") || !now.Before(invitation.ExpiresAt) {
			return domaininvitation.ErrInvalidInvitation
		}
		if strings.TrimSpace(invitation.InvitedEmail) != "" &&
			!strings.EqualFold(strings.TrimSpace(invitation.InvitedEmail), strings.TrimSpace(normalizedEmail)) {
			return domaininvitation.ErrInvalidInvitation
		}

		member, err := addActiveMember(ctx, tx, invitation.SpaceID, userID, invitation.RoleID, now, domaininvitation.ErrInvalidInvitation)
		if err != nil {
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
			RoleName:     invitation.RoleName,
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

// addActiveMember locks the SHARED, ACTIVE Space row FOR UPDATE and inserts an
// ACTIVE membership. errSpaceUnavailable is returned when the Space is missing,
// PERSONAL, inactive or deleted; a duplicate membership maps to
// ErrMembershipConflict. It is shared by Accept and AddMember.
func addActiveMember(ctx context.Context, tx *gorm.DB, spaceID, userID, roleID string, now time.Time, errSpaceUnavailable error) (*domainspace.Member, error) {
	var space domainspace.Space
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND type = ? AND status = ? AND deleted_at IS NULL", spaceID, domainspace.TypeShared, domainspace.StatusActive).
		Take(&space).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errSpaceUnavailable
	}
	if err != nil {
		return nil, err
	}
	if space.Type != domainspace.TypeShared || space.Status != domainspace.StatusActive || space.DeletedAt.Valid {
		return nil, errSpaceUnavailable
	}

	member := &domainspace.Member{
		ID:        utils.CreateUUID(),
		SpaceID:   spaceID,
		UserID:    strings.TrimSpace(userID),
		RoleID:    roleID,
		Status:    domainspace.StatusActive,
		CreatedAt: now,
	}
	if err := tx.WithContext(ctx).Create(member).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, domaininvitation.ErrMembershipConflict
		}
		return nil, err
	}
	return member, nil
}

// FindUserByVerifiedPhone applies the same rule as onboarding.FindByPhone: only a
// verified, non-deleted account is found.
func (r *Repository) FindUserByVerifiedPhone(ctx context.Context, phone string) (*domaininvitation.UserRef, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return nil, domainidentity.ErrIdentityNotFound
	}
	var user domaininvitation.UserRef
	err := r.DB.WithContext(ctx).
		Table("users u").
		Select("u.id::text AS id, u.name AS name").
		Where("u.phone = ? AND u.phone_verified_at IS NOT NULL AND u.deleted_at IS NULL", phone).
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainidentity.ErrIdentityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// AddMember adds an existing account to a SHARED, ACTIVE Space in one
// transaction, with the same Space lock and duplicate handling as Accept but no
// invitation row. A missing or non-shared Space yields gorm.ErrRecordNotFound.
func (r *Repository) AddMember(ctx context.Context, spaceID, userID, roleID string, now time.Time) (*domainspace.Member, error) {
	spaceID, userID, roleID = strings.TrimSpace(spaceID), strings.TrimSpace(userID), strings.TrimSpace(roleID)
	if spaceID == "" || userID == "" || roleID == "" {
		return nil, errors.New("space, user and role are required")
	}
	var member *domainspace.Member
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		member, err = addActiveMember(ctx, tx, spaceID, userID, roleID, now, gorm.ErrRecordNotFound)
		return err
	})
	if err != nil {
		return nil, err
	}
	return member, nil
}

func (r *Repository) ListPending(ctx context.Context, spaceID string) ([]domaininvitation.Invitation, error) {
	var invitations []domaininvitation.Invitation
	err := r.DB.WithContext(ctx).
		Table("space_invitations").
		Select("space_invitations.id, space_invitations.space_id, space_invitations.invited_email, space_invitations.role_id, space_invitations.invited_by_member_id, space_invitations.status, space_invitations.expires_at, space_invitations.accepted_at, space_invitations.accepted_by_user_id, space_invitations.created_at, space_invitations.updated_at, space_invitations.deleted_at, roles.name AS role_name").
		Joins("JOIN roles ON roles.id = space_invitations.role_id AND roles.deleted_at IS NULL").
		Where("space_invitations.space_id = ? AND space_invitations.status = ? AND space_invitations.deleted_at IS NULL", spaceID, domaininvitation.StatusPending).
		Order("space_invitations.created_at ASC").
		Find(&invitations).Error
	return invitations, err
}

func (r *Repository) RevokePending(ctx context.Context, spaceID, invitationID string, revokedAt time.Time) error {
	result := r.DB.WithContext(ctx).Model(&domaininvitation.Invitation{}).
		Where("id = ? AND space_id = ? AND status = ? AND deleted_at IS NULL", invitationID, spaceID, domaininvitation.StatusPending).
		Updates(map[string]any{
			"status":     domaininvitation.StatusRevoked,
			"deleted_at": revokedAt,
			"updated_at": revokedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
