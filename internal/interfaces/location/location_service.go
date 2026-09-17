package interfacelocation

import "context"
import "starter-kit/internal/dto"

type ServiceLocationInterface interface {
	GetProvince(ctx context.Context) ([]dto.Location, error)
	GetCity(ctx context.Context, provinceCode string) ([]dto.Location, error)
	GetDistrict(ctx context.Context, cityCode string) ([]dto.Location, error)
	GetVillage(ctx context.Context, districtCode string) ([]dto.Location, error)
	StartSync(ctx context.Context, req dto.SyncLocationRequest) (dto.LocationSyncJob, error)
	GetSyncJob(ctx context.Context, id string) (dto.LocationSyncJob, error)
}
