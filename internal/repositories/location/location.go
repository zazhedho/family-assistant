package repositorylocation

import (
	"context"
	domainlocation "starter-kit/internal/domain/location"
	interfacelocation "starter-kit/internal/interfaces/location"
	"starter-kit/utils"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type repo struct {
	DB *gorm.DB
}

func NewLocationRepo(db *gorm.DB) interfacelocation.RepoLocationInterface {
	return &repo{DB: db}
}

func (r *repo) ListProvinces(ctx context.Context) (ret []domainlocation.Province, err error) {
	err = r.DB.WithContext(ctx).Where("deleted_at IS NULL").Order("name ASC").Find(&ret).Error
	return
}

func (r *repo) ListCitiesByProvince(ctx context.Context, provinceCode string) (ret []domainlocation.City, err error) {
	err = r.DB.WithContext(ctx).Where("province_code = ? AND deleted_at IS NULL", provinceCode).Order("name ASC").Find(&ret).Error
	return
}

func (r *repo) ListDistrictsByCity(ctx context.Context, cityCode string) (ret []domainlocation.District, err error) {
	err = r.DB.WithContext(ctx).Where("city_code = ? AND deleted_at IS NULL", cityCode).Order("name ASC").Find(&ret).Error
	return
}

func (r *repo) ListVillagesByDistrict(ctx context.Context, districtCode string) (ret []domainlocation.Village, err error) {
	err = r.DB.WithContext(ctx).Where("district_code = ? AND deleted_at IS NULL", districtCode).Order("name ASC").Find(&ret).Error
	return
}

func (r *repo) GetProvinceByCode(ctx context.Context, code string) (ret domainlocation.Province, err error) {
	err = r.DB.WithContext(ctx).Where("code = ? AND deleted_at IS NULL", code).First(&ret).Error
	return
}

func (r *repo) GetCityByCode(ctx context.Context, code string) (ret domainlocation.City, err error) {
	err = r.DB.WithContext(ctx).Where("code = ? AND deleted_at IS NULL", code).First(&ret).Error
	return
}

func (r *repo) GetDistrictByCode(ctx context.Context, code string) (ret domainlocation.District, err error) {
	err = r.DB.WithContext(ctx).Where("code = ? AND deleted_at IS NULL", code).First(&ret).Error
	return
}

func (r *repo) UpsertProvinces(ctx context.Context, items []domainlocation.Province) error {
	return r.upsert(ctx, "code", items)
}

func (r *repo) UpsertCities(ctx context.Context, items []domainlocation.City) error {
	return r.upsert(ctx, "code", items)
}

func (r *repo) UpsertDistricts(ctx context.Context, items []domainlocation.District) error {
	return r.upsert(ctx, "code", items)
}

func (r *repo) UpsertVillages(ctx context.Context, items []domainlocation.Village) error {
	return r.upsert(ctx, "code", items)
}

func (r *repo) CreateSyncJob(ctx context.Context, job *domainlocation.SyncJob) error {
	return r.DB.WithContext(ctx).Create(job).Error
}

func (r *repo) UpdateSyncJob(ctx context.Context, job *domainlocation.SyncJob) error {
	return r.DB.WithContext(ctx).Save(job).Error
}

func (r *repo) GetSyncJobByID(ctx context.Context, id string) (ret domainlocation.SyncJob, err error) {
	err = r.DB.WithContext(ctx).Where("id = ?", id).First(&ret).Error
	return
}

func (r *repo) GetActiveSyncJob(ctx context.Context) (ret domainlocation.SyncJob, err error) {
	err = r.DB.WithContext(ctx).
		Where("status IN ?", []string{"queued", "running"}).
		Order("created_at ASC").
		First(&ret).Error
	return
}

func (r *repo) FailActiveSyncJobs(ctx context.Context, message string) error {
	now := time.Now()
	return r.DB.WithContext(ctx).Model(&domainlocation.SyncJob{}).
		Where("status IN ?", []string{"queued", "running"}).
		Updates(map[string]any{
			"status":        "failed",
			"message":       "Location sync interrupted",
			"error_message": message,
			"finished_at":   now,
			"updated_at":    now,
		}).Error
}

func (r *repo) upsert(ctx context.Context, conflictColumn string, values any) error {
	now := time.Now()

	switch items := values.(type) {
	case []domainlocation.Province:
		for i := range items {
			if items[i].ID == "" {
				items[i].ID = utils.CreateUUID()
			}
			if items[i].CreatedAt.IsZero() {
				items[i].CreatedAt = now
			}
			items[i].UpdatedAt = &now
		}
		return r.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: conflictColumn}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "updated_at", "deleted_at"}),
		}).Create(&items).Error
	case []domainlocation.City:
		for i := range items {
			if items[i].ID == "" {
				items[i].ID = utils.CreateUUID()
			}
			if items[i].CreatedAt.IsZero() {
				items[i].CreatedAt = now
			}
			items[i].UpdatedAt = &now
		}
		return r.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: conflictColumn}},
			DoUpdates: clause.AssignmentColumns([]string{"province_code", "name", "updated_at", "deleted_at"}),
		}).Create(&items).Error
	case []domainlocation.District:
		for i := range items {
			if items[i].ID == "" {
				items[i].ID = utils.CreateUUID()
			}
			if items[i].CreatedAt.IsZero() {
				items[i].CreatedAt = now
			}
			items[i].UpdatedAt = &now
		}
		return r.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: conflictColumn}},
			DoUpdates: clause.AssignmentColumns([]string{"city_code", "name", "updated_at", "deleted_at"}),
		}).Create(&items).Error
	case []domainlocation.Village:
		for i := range items {
			if items[i].ID == "" {
				items[i].ID = utils.CreateUUID()
			}
			if items[i].CreatedAt.IsZero() {
				items[i].CreatedAt = now
			}
			items[i].UpdatedAt = &now
		}
		return r.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: conflictColumn}},
			DoUpdates: clause.AssignmentColumns([]string{"district_code", "name", "updated_at", "deleted_at"}),
		}).Create(&items).Error
	default:
		return nil
	}
}
