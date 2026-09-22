package repositoryactivity

import (
	"context"
	"strings"

	domainactivity "family-assistant/internal/domain/activity"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	"family-assistant/utils"

	"gorm.io/gorm"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfaceactivity.RepoActivityInterface {
	return &Repository{DB: db}
}

func (r *Repository) Create(ctx context.Context, activity *domainactivity.Activity) error {
	if activity == nil {
		return domainactivity.ErrActivityRequired
	}
	if strings.TrimSpace(activity.SpaceID) == "" {
		return domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activity.ID) == "" {
		activity.ID = utils.CreateUUID()
	}
	return r.DB.WithContext(ctx).
		Select("id", "space_id", "created_by_member_id", "kind", "note", "occurred_at", "created_at", "updated_at").
		Create(activity).Error
}

func (r *Repository) List(ctx context.Context, filter domainactivity.ListFilter) ([]domainactivity.Activity, error) {
	if strings.TrimSpace(filter.SpaceID) == "" {
		return nil, domainactivity.ErrActivitySpaceID
	}
	query := r.DB.WithContext(ctx).Where("space_id = ?", strings.TrimSpace(filter.SpaceID))
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if filter.From != nil {
		query = query.Where("occurred_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("occurred_at <= ?", *filter.To)
	}
	query = query.Order("occurred_at DESC")
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	var activities []domainactivity.Activity
	if err := query.Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}

var _ interfaceactivity.RepoActivityInterface = (*Repository)(nil)
