package repositoryfamilymember

import (
	"context"

	domainfamily "family-assistant/internal/domain/family"
	domainfamilymember "family-assistant/internal/domain/familymember"

	"gorm.io/gorm"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{DB: db}
}

var _ domainfamilymember.Repository = (*Repository)(nil)

func (r *Repository) FindActiveByHermesProfile(ctx context.Context, profileID string) (*domainfamilymember.ResolvedMember, error) {
	return findExactlyOneActiveMember(r.DB.WithContext(ctx).
		Table("family_members fm").
		Select("fm.user_id, fm.id AS member_id, fm.family_id, fm.role_id, r.name AS role_name, u.hermes_profile_id").
		Joins("JOIN users u ON u.id = fm.user_id").
		Joins("JOIN families f ON f.id = fm.family_id").
		Joins("JOIN roles r ON r.id = fm.role_id").
		Where("u.hermes_profile_id = ? AND fm.status = ? AND f.status = ? AND u.deleted_at IS NULL AND r.deleted_at IS NULL", profileID, domainfamilymember.StatusActive, domainfamily.StatusActive))
}

func (r *Repository) FindActiveByUserID(ctx context.Context, userID string) (*domainfamilymember.ResolvedMember, error) {
	return findExactlyOneActiveMember(r.DB.WithContext(ctx).
		Table("family_members fm").
		Select("fm.user_id, fm.id AS member_id, fm.family_id, fm.role_id, r.name AS role_name, COALESCE(u.hermes_profile_id, '') AS hermes_profile_id").
		Joins("JOIN users u ON u.id = fm.user_id").
		Joins("JOIN families f ON f.id = fm.family_id").
		Joins("JOIN roles r ON r.id = fm.role_id").
		Where("u.id = ? AND fm.status = ? AND f.status = ? AND u.deleted_at IS NULL AND r.deleted_at IS NULL", userID, domainfamilymember.StatusActive, domainfamily.StatusActive))
}

func findExactlyOneActiveMember(query *gorm.DB) (*domainfamilymember.ResolvedMember, error) {
	var members []domainfamilymember.ResolvedMember
	err := query.Limit(2).Find(&members).Error
	if err != nil {
		return nil, err
	}
	if len(members) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return &members[0], nil
}

func (r *Repository) FindActiveByID(ctx context.Context, familyID, memberID string) (*domainfamilymember.FamilyMember, error) {
	var member domainfamilymember.FamilyMember
	err := r.DB.WithContext(ctx).
		Table("family_members fm").
		Select("fm.id, fm.family_id, fm.user_id, fm.role_id, fm.status, fm.created_at, fm.updated_at, r.name AS role_name").
		Joins("JOIN roles r ON r.id = fm.role_id").
		Where("fm.family_id = ? AND fm.id = ? AND fm.status = ? AND r.deleted_at IS NULL", familyID, memberID, domainfamilymember.StatusActive).
		First(&member).Error
	if err != nil {
		return nil, err
	}

	return &member, nil
}
