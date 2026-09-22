package repositoryspace

import (
	"context"
	"errors"
	"strings"
	"time"

	domainspace "family-assistant/internal/domain/space"
	interfacespace "family-assistant/internal/interfaces/space"
	"family-assistant/utils"

	"gorm.io/gorm"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfacespace.RepoSpaceInterface {
	return &Repository{DB: db}
}

var _ interfacespace.RepoSpaceInterface = (*Repository)(nil)

var (
	errSpaceRequired       = errors.New("space is required")
	errMemberRequired      = errors.New("member is required")
	errMemberSpaceMismatch = errors.New("member Space ID does not match Space ID")
)

func (r *Repository) CreateWithOwner(ctx context.Context, space *domainspace.Space, member *domainspace.Member) error {
	if space == nil {
		return errSpaceRequired
	}
	if member == nil {
		return errMemberRequired
	}
	spaceID := strings.TrimSpace(space.ID)
	memberSpaceID := strings.TrimSpace(member.SpaceID)
	if memberSpaceID != "" && memberSpaceID != spaceID {
		return errMemberSpaceMismatch
	}
	if strings.TrimSpace(space.ID) == "" {
		space.ID = utils.CreateUUID()
	}
	if strings.TrimSpace(member.ID) == "" {
		member.ID = utils.CreateUUID()
	}
	if memberSpaceID == "" {
		member.SpaceID = space.ID
	}

	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(space).Error; err != nil {
			return err
		}
		return tx.Create(member).Error
	})
}

func (r *Repository) ListActiveByUserID(ctx context.Context, userID string) ([]domainspace.ResolvedMembership, error) {
	var memberships []domainspace.ResolvedMembership
	err := activeMembershipQuery(r.DB.WithContext(ctx)).
		Where("sm.user_id = ? AND sm.status = ? AND sm.deleted_at IS NULL AND s.status = ? AND s.deleted_at IS NULL", userID, domainspace.StatusActive, domainspace.StatusActive).
		Find(&memberships).Error
	return memberships, err
}

func (r *Repository) FindActiveMembership(ctx context.Context, userID, spaceID string) (*domainspace.ResolvedMembership, error) {
	var membership domainspace.ResolvedMembership
	err := activeMembershipQuery(r.DB.WithContext(ctx)).
		Where("sm.user_id = ? AND sm.space_id = ? AND sm.status = ? AND sm.deleted_at IS NULL AND s.status = ? AND s.deleted_at IS NULL", userID, spaceID, domainspace.StatusActive, domainspace.StatusActive).
		Take(&membership).Error
	if err != nil {
		return nil, err
	}
	return &membership, nil
}

func (r *Repository) ListActiveMembers(ctx context.Context, spaceID string) ([]domainspace.ResolvedMembership, error) {
	var memberships []domainspace.ResolvedMembership
	err := activeMembershipQuery(r.DB.WithContext(ctx)).
		Where("sm.space_id = ? AND sm.status = ? AND sm.deleted_at IS NULL AND s.status = ? AND s.deleted_at IS NULL", spaceID, domainspace.StatusActive, domainspace.StatusActive).
		Find(&memberships).Error
	return memberships, err
}

func (r *Repository) Update(ctx context.Context, spaceID string, fields domainspace.SpaceUpdateFields, updatedAt time.Time) error {
	updates := map[string]any{"updated_at": updatedAt}
	if fields.Name != nil {
		updates["name"] = *fields.Name
	}
	if fields.Category != nil {
		updates["category"] = *fields.Category
	}
	result := r.DB.WithContext(ctx).Model(&domainspace.Space{}).
		Where("id = ? AND status = ? AND deleted_at IS NULL", spaceID, domainspace.StatusActive).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) Archive(ctx context.Context, spaceID string, archivedAt time.Time) error {
	result := r.DB.WithContext(ctx).Model(&domainspace.Space{}).
		Where("id = ? AND status = ? AND deleted_at IS NULL", spaceID, domainspace.StatusActive).
		Updates(map[string]any{
			"status":     domainspace.StatusArchived,
			"deleted_at": archivedAt,
			"updated_at": archivedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) UpdateMemberRole(ctx context.Context, spaceID, memberID, roleID string, updatedAt time.Time) error {
	result := r.DB.WithContext(ctx).Model(&domainspace.Member{}).
		Where("id = ? AND space_id = ? AND status = ? AND deleted_at IS NULL", memberID, spaceID, domainspace.StatusActive).
		Updates(map[string]any{"role_id": roleID, "updated_at": updatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) RemoveMember(ctx context.Context, spaceID, memberID string, removedAt time.Time) error {
	result := r.DB.WithContext(ctx).Model(&domainspace.Member{}).
		Where("id = ? AND space_id = ? AND status = ? AND deleted_at IS NULL", memberID, spaceID, domainspace.StatusActive).
		Updates(map[string]any{
			"status":     domainspace.StatusInactive,
			"deleted_at": removedAt,
			"updated_at": removedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) CountActiveOwners(ctx context.Context, spaceID string) (int64, error) {
	var count int64
	err := r.DB.WithContext(ctx).
		Table("space_members sm").
		Joins("JOIN roles r ON r.id = sm.role_id").
		Where("sm.space_id = ? AND r.name = ? AND sm.status = ? AND sm.deleted_at IS NULL AND r.deleted_at IS NULL", spaceID, spaceOwnerRoleName, domainspace.StatusActive).
		Count(&count).Error
	return count, err
}

const spaceOwnerRoleName = "space_owner"

func activeMembershipQuery(db *gorm.DB) *gorm.DB {
	return db.
		Table("space_members sm").
		Select("sm.id, sm.space_id, s.name AS space_name, s.type AS space_type, s.category AS space_category, sm.user_id, u.name AS user_name, u.email AS user_email, sm.role_id, r.name AS role_name, sm.status").
		Joins("JOIN spaces s ON s.id = sm.space_id").
		Joins("JOIN users u ON u.id = sm.user_id").
		Joins("JOIN roles r ON r.id = sm.role_id").
		Where("u.deleted_at IS NULL AND r.deleted_at IS NULL")
}
