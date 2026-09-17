package interfacelocation

import "context"
import domainlocation "starter-kit/internal/domain/location"

type RepoLocationInterface interface {
	ListProvinces(ctx context.Context) ([]domainlocation.Province, error)
	ListCitiesByProvince(ctx context.Context, provinceCode string) ([]domainlocation.City, error)
	ListDistrictsByCity(ctx context.Context, cityCode string) ([]domainlocation.District, error)
	ListVillagesByDistrict(ctx context.Context, districtCode string) ([]domainlocation.Village, error)
	GetProvinceByCode(ctx context.Context, code string) (domainlocation.Province, error)
	GetCityByCode(ctx context.Context, code string) (domainlocation.City, error)
	GetDistrictByCode(ctx context.Context, code string) (domainlocation.District, error)
	UpsertProvinces(ctx context.Context, items []domainlocation.Province) error
	UpsertCities(ctx context.Context, items []domainlocation.City) error
	UpsertDistricts(ctx context.Context, items []domainlocation.District) error
	UpsertVillages(ctx context.Context, items []domainlocation.Village) error
	CreateSyncJob(ctx context.Context, job *domainlocation.SyncJob) error
	UpdateSyncJob(ctx context.Context, job *domainlocation.SyncJob) error
	GetSyncJobByID(ctx context.Context, id string) (domainlocation.SyncJob, error)
	GetActiveSyncJob(ctx context.Context) (domainlocation.SyncJob, error)
	FailActiveSyncJobs(ctx context.Context, message string) error
}
