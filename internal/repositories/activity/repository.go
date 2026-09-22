package repositoryactivity

import (
	"context"
	"strings"
	"time"

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

func (r *Repository) FindByIDInSpace(ctx context.Context, spaceID, activityID string) (*domainactivity.Activity, error) {
	if strings.TrimSpace(spaceID) == "" {
		return nil, domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activityID) == "" {
		return nil, domainactivity.ErrActivityIDRequired
	}
	var activity domainactivity.Activity
	if err := r.DB.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, activityID).First(&activity).Error; err != nil {
		return nil, err
	}
	return &activity, nil
}

func (r *Repository) Update(ctx context.Context, spaceID, activityID string, fields domainactivity.UpdateFields, updatedAt time.Time) error {
	if strings.TrimSpace(spaceID) == "" {
		return domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activityID) == "" {
		return domainactivity.ErrActivityIDRequired
	}
	updates := map[string]any{"updated_at": updatedAt}
	if fields.Kind != nil {
		updates["kind"] = strings.TrimSpace(*fields.Kind)
	}
	if fields.Note != nil {
		updates["note"] = strings.TrimSpace(*fields.Note)
	}
	if fields.OccurredAt != nil {
		updates["occurred_at"] = *fields.OccurredAt
	}
	if len(updates) == 1 {
		return domainactivity.ErrActivityUpdateRequired
	}
	result := r.DB.WithContext(ctx).Model(&domainactivity.Activity{}).
		Where("id = ? AND space_id = ?", activityID, spaceID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) SoftDelete(ctx context.Context, spaceID, activityID string, deletedAt time.Time) error {
	if strings.TrimSpace(spaceID) == "" {
		return domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activityID) == "" {
		return domainactivity.ErrActivityIDRequired
	}
	result := r.DB.WithContext(ctx).Model(&domainactivity.Activity{}).
		Where("id = ? AND space_id = ?", activityID, spaceID).
		Updates(map[string]any{"deleted_at": deletedAt, "updated_at": deletedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

var _ interfaceactivity.RepoActivityInterface = (*Repository)(nil)
