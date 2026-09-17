package repositoryfamilymember

import (
	"context"

	domainfamily "github.com/zazhedho/family-assistant/internal/domain/family"
	domainfamilymember "github.com/zazhedho/family-assistant/internal/domain/familymember"
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
	var member domainfamilymember.ResolvedMember
	err := r.DB.WithContext(ctx).
		Table("family_members fm").
		Select("fm.user_id, fm.id AS member_id, fm.family_id, fm.role_id, r.name AS role_name, u.hermes_profile_id").
		Joins("JOIN users u ON u.id = fm.user_id").
		Joins("JOIN families f ON f.id = fm.family_id").
		Joins("JOIN roles r ON r.id = fm.role_id").
		Where("u.hermes_profile_id = ? AND fm.status = ? AND f.status = ? AND u.deleted_at IS NULL AND r.deleted_at IS NULL", profileID, domainfamilymember.StatusActive, domainfamily.StatusActive).
		First(&member).Error
	if err != nil {
		return nil, err
	}

	return &member, nil
}

func (r *Repository) FindActiveByID(ctx context.Context, familyID, memberID string) (*domainfamilymember.FamilyMember, error) {
	var member domainfamilymember.FamilyMember
	err := r.DB.WithContext(ctx).
		Where("family_id = ? AND id = ? AND status = ?", familyID, memberID, domainfamilymember.StatusActive).
		First(&member).Error
	if err != nil {
		return nil, err
	}

	return &member, nil
}
