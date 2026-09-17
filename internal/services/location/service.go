package servicelocation

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"starter-kit/internal/authscope"
	locationcache "starter-kit/internal/cache/location"
	domainlocation "starter-kit/internal/domain/location"
	"starter-kit/internal/dto"
	interfacelocation "starter-kit/internal/interfaces/location"
	"starter-kit/pkg/logger"
	"starter-kit/utils"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const defaultLocationServiceBaseURL = "https://location-service-y7si.onrender.com"

type LocationService struct {
	Repo       interfacelocation.RepoLocationInterface
	Redis      *redis.Client
	HTTPClient *http.Client
	syncing    atomic.Bool
}

type syncProgress struct {
	Message       string
	ProvinceCount int
	CityCount     int
	DistrictCount int
	VillageCount  int
}

var ErrLocationSyncRunning = errors.New("location sync is already running")

func NewLocationService(repo interfacelocation.RepoLocationInterface, redisClients ...*redis.Client) *LocationService {
	var redisClient *redis.Client
	if len(redisClients) > 0 {
		redisClient = redisClients[0]
	}

	service := &LocationService{
		Repo:       repo,
		Redis:      redisClient,
		HTTPClient: &http.Client{Timeout: locationServiceTimeout()},
	}

	if err := service.Repo.FailActiveSyncJobs(context.Background(), "Service restarted before the previous location sync completed."); err != nil {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("failed to mark interrupted location sync jobs: %v", err))
	}

	return service
}

func (s *LocationService) GetProvince(ctx context.Context) ([]dto.Location, error) {
	cacheKey := locationcache.ProvinceKey()
	if data, ok := locationcache.Get(ctx, s.Redis, cacheKey); ok {
		if len(data) > 0 {
			return data, nil
		}
	}

	rows, err := s.Repo.ListProvinces(ctx)
	if err != nil {
		return nil, err
	}

	locations := mapProvinces(rows)
	if len(locations) == 0 {
		fetched, fetchErr := s.fetchProvinces(ctx, "")
		if fetchErr != nil {
			return nil, fetchErr
		}
		if err := s.Repo.UpsertProvinces(ctx, fetched); err != nil {
			return nil, err
		}
		locations = mapProvinces(fetched)
	}
	setLocationCache(ctx, s.Redis, cacheKey, locations)
	return locations, nil
}

func (s *LocationService) GetCity(ctx context.Context, provinceCode string) ([]dto.Location, error) {
	cacheKey := locationcache.CityKey(provinceCode)
	if data, ok := locationcache.Get(ctx, s.Redis, cacheKey); ok {
		if len(data) > 0 {
			return data, nil
		}
	}

	rows, err := s.Repo.ListCitiesByProvince(ctx, provinceCode)
	if err != nil {
		return nil, err
	}

	locations := mapCities(rows)
	if len(locations) == 0 {
		fetched, fetchErr := s.fetchCities(ctx, "", provinceCode)
		if fetchErr != nil {
			return nil, fetchErr
		}
		if err := s.Repo.UpsertCities(ctx, fetched); err != nil {
			return nil, err
		}
		locations = mapCities(fetched)
	}
	setLocationCache(ctx, s.Redis, cacheKey, locations)
	return locations, nil
}

func (s *LocationService) GetDistrict(ctx context.Context, cityCode string) ([]dto.Location, error) {
	cacheKey := locationcache.DistrictKey(cityCode)
	if data, ok := locationcache.Get(ctx, s.Redis, cacheKey); ok {
		if len(data) > 0 {
			return data, nil
		}
	}

	rows, err := s.Repo.ListDistrictsByCity(ctx, cityCode)
	if err != nil {
		return nil, err
	}

	locations := mapDistricts(rows)
	if len(locations) == 0 {
		city, cityErr := s.Repo.GetCityByCode(ctx, cityCode)
		if cityErr != nil {
			return nil, cityErr
		}
		fetched, fetchErr := s.fetchDistricts(ctx, "", city.ProvinceCode, cityCode)
		if fetchErr != nil {
			return nil, fetchErr
		}
		if err := s.Repo.UpsertDistricts(ctx, fetched); err != nil {
			return nil, err
		}
		locations = mapDistricts(fetched)
	}
	setLocationCache(ctx, s.Redis, cacheKey, locations)
	return locations, nil
}

func (s *LocationService) GetVillage(ctx context.Context, districtCode string) ([]dto.Location, error) {
	cacheKey := locationcache.VillageKey(districtCode)
	if data, ok := locationcache.Get(ctx, s.Redis, cacheKey); ok {
		if len(data) > 0 {
			return data, nil
		}
	}

	rows, err := s.Repo.ListVillagesByDistrict(ctx, districtCode)
	if err != nil {
		return nil, err
	}

	locations := mapVillages(rows)
	if len(locations) == 0 {
		district, districtErr := s.Repo.GetDistrictByCode(ctx, districtCode)
		if districtErr != nil {
			return nil, districtErr
		}
		city, cityErr := s.Repo.GetCityByCode(ctx, district.CityCode)
		if cityErr != nil {
			return nil, cityErr
		}
		fetched, fetchErr := s.fetchVillages(ctx, "", city.ProvinceCode, district.CityCode, districtCode)
		if fetchErr != nil {
			return nil, fetchErr
		}
		if err := s.Repo.UpsertVillages(ctx, fetched); err != nil {
			return nil, err
		}
		locations = mapVillages(fetched)
	}
	setLocationCache(ctx, s.Redis, cacheKey, locations)
	return locations, nil
}

func (s *LocationService) StartSync(ctx context.Context, req dto.SyncLocationRequest) (dto.LocationSyncJob, error) {
	normalizedReq, err := s.normalizeAndValidateRequest(req)
	if err != nil {
		return dto.LocationSyncJob{}, err
	}
	requestedByUserID := authscope.FromContext(ctx).ActorUserID()

	activeJob, err := s.Repo.GetActiveSyncJob(ctx)
	switch {
	case err == nil:
		return mapSyncJob(activeJob), ErrLocationSyncRunning
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return dto.LocationSyncJob{}, err
	}

	now := time.Now()
	job := domainlocation.SyncJob{
		ID:           utils.CreateUUID(),
		Status:       "queued",
		Level:        normalizedReq.Level,
		Year:         normalizedReq.Year,
		ProvinceCode: normalizedReq.ProvinceCode,
		CityCode:     normalizedReq.CityCode,
		DistrictCode: normalizedReq.DistrictCode,
		RequestedBy:  requestedByUserID,
		Message:      "Location sync queued",
		CreatedAt:    now,
		UpdatedAt:    &now,
	}

	if err := s.Repo.CreateSyncJob(ctx, &job); err != nil {
		return dto.LocationSyncJob{}, err
	}

	go s.runSyncJob(job.ID, normalizedReq)

	return mapSyncJob(job), nil
}

func (s *LocationService) GetSyncJob(ctx context.Context, id string) (dto.LocationSyncJob, error) {
	job, err := s.Repo.GetSyncJobByID(ctx, id)
	if err != nil {
		return dto.LocationSyncJob{}, err
	}

	return mapSyncJob(job), nil
}

func (s *LocationService) normalizeAndValidateRequest(req dto.SyncLocationRequest) (dto.SyncLocationRequest, error) {
	req.Level = normalizeSyncLevel(req.Level)
	req.Year = normalizeSyncYear(req.Year)

	switch req.Level {
	case "province":
		return req, nil
	case "city":
		if req.ProvinceCode == "" {
			return dto.SyncLocationRequest{}, errors.New("province_code is required for city sync")
		}
		return req, nil
	case "district":
		if req.ProvinceCode == "" || req.CityCode == "" {
			return dto.SyncLocationRequest{}, errors.New("province_code and city_code are required for district sync")
		}
		return req, nil
	case "village":
		if req.ProvinceCode == "" || req.CityCode == "" || req.DistrictCode == "" {
			return dto.SyncLocationRequest{}, errors.New("province_code, city_code, and district_code are required for village sync")
		}
		return req, nil
	case "all":
		return req, nil
	default:
		return dto.SyncLocationRequest{}, errors.New("invalid sync level")
	}
}

func (s *LocationService) runSyncJob(jobID string, req dto.SyncLocationRequest) {
	jobCtx := context.Background()
	if !s.syncing.CompareAndSwap(false, true) {
		s.markSyncJobFailed(jobCtx, jobID, "Another location sync is already running in this service instance.")
		return
	}
	defer s.syncing.Store(false)

	job, err := s.Repo.GetSyncJobByID(jobCtx, jobID)
	if err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("failed to load location sync job %s: %v", jobID, err))
		return
	}

	startedAt := time.Now()
	job.Status = "running"
	job.Message = "Location sync is running"
	job.StartedAt = &startedAt
	job.UpdatedAt = &startedAt
	if err := s.Repo.UpdateSyncJob(jobCtx, &job); err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("failed to mark location sync job %s as running: %v", jobID, err))
		return
	}

	result, err := s.sync(jobCtx, req, func(progress syncProgress) {
		s.applySyncProgress(&job, progress)
		if updateErr := s.Repo.UpdateSyncJob(jobCtx, &job); updateErr != nil {
			logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("failed to update location sync job progress %s: %v", job.ID, updateErr))
		}
	})
	if err != nil {
		s.markSyncJobFailed(jobCtx, job.ID, err.Error())
		return
	}

	s.applySyncProgress(&job, result)
	finishedAt := time.Now()
	job.Status = "completed"
	job.Message = "Location sync completed"
	job.ErrorMessage = ""
	job.FinishedAt = &finishedAt
	job.UpdatedAt = &finishedAt
	if err := s.Repo.UpdateSyncJob(jobCtx, &job); err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("failed to mark location sync job %s as completed: %v", job.ID, err))
	}
}

func (s *LocationService) markSyncJobFailed(ctx context.Context, jobID, errorMessage string) {
	job, err := s.Repo.GetSyncJobByID(ctx, jobID)
	if err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("failed to load location sync job %s for failure update: %v", jobID, err))
		return
	}

	finishedAt := time.Now()
	job.Status = "failed"
	job.Message = "Location sync failed"
	job.ErrorMessage = errorMessage
	job.FinishedAt = &finishedAt
	job.UpdatedAt = &finishedAt
	if err := s.Repo.UpdateSyncJob(ctx, &job); err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("failed to mark location sync job %s as failed: %v", job.ID, err))
	}
}

func (s *LocationService) applySyncProgress(job *domainlocation.SyncJob, progress syncProgress) {
	job.Message = progress.Message
	job.ProvinceCount = progress.ProvinceCount
	job.CityCount = progress.CityCount
	job.DistrictCount = progress.DistrictCount
	job.VillageCount = progress.VillageCount
	job.UpdatedAt = new(time.Now())
}

func (s *LocationService) sync(ctx context.Context, req dto.SyncLocationRequest, progress func(syncProgress)) (syncProgress, error) {
	switch req.Level {
	case "province":
		progress(syncProgress{Message: "Fetching provinces"})
		provinces, err := s.fetchProvinces(ctx, req.Year)
		if err != nil {
			return syncProgress{}, err
		}
		if err := s.Repo.UpsertProvinces(ctx, provinces); err != nil {
			return syncProgress{}, err
		}
		locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.ProvinceKey())
		return syncProgress{
			Message:       "Province sync completed",
			ProvinceCount: len(provinces),
		}, nil
	case "city":
		progress(syncProgress{Message: fmt.Sprintf("Fetching cities for province %s", req.ProvinceCode)})
		cities, err := s.fetchCities(ctx, req.Year, req.ProvinceCode)
		if err != nil {
			return syncProgress{}, err
		}
		if err := s.Repo.UpsertCities(ctx, cities); err != nil {
			return syncProgress{}, err
		}
		locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.CityKey(req.ProvinceCode))
		return syncProgress{
			Message:   "City sync completed",
			CityCount: len(cities),
		}, nil
	case "district":
		progress(syncProgress{Message: fmt.Sprintf("Fetching districts for city %s", req.CityCode)})
		districts, err := s.fetchDistricts(ctx, req.Year, req.ProvinceCode, req.CityCode)
		if err != nil {
			return syncProgress{}, err
		}
		if err := s.Repo.UpsertDistricts(ctx, districts); err != nil {
			return syncProgress{}, err
		}
		locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.DistrictKey(req.CityCode))
		return syncProgress{
			Message:       "District sync completed",
			DistrictCount: len(districts),
		}, nil
	case "village":
		progress(syncProgress{Message: fmt.Sprintf("Fetching villages for district %s", req.DistrictCode)})
		villages, err := s.fetchVillages(ctx, req.Year, req.ProvinceCode, req.CityCode, req.DistrictCode)
		if err != nil {
			return syncProgress{}, err
		}
		if err := s.Repo.UpsertVillages(ctx, villages); err != nil {
			return syncProgress{}, err
		}
		locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.VillageKey(req.DistrictCode))
		return syncProgress{
			Message:      "Village sync completed",
			VillageCount: len(villages),
		}, nil
	case "all":
		return s.syncAll(ctx, req.Year, progress)
	default:
		return syncProgress{}, errors.New("invalid sync level")
	}
}

func (s *LocationService) syncAll(ctx context.Context, year string, progress func(syncProgress)) (syncProgress, error) {
	progress(syncProgress{Message: "Fetching provinces"})
	provinces, err := s.fetchProvinces(ctx, year)
	if err != nil {
		return syncProgress{}, err
	}
	if err := s.Repo.UpsertProvinces(ctx, provinces); err != nil {
		return syncProgress{}, err
	}
	locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.ProvinceKey())

	var (
		cityCount     int
		districtCount int
		villageCount  int
	)
	progress(syncProgress{
		Message:       "Provinces synced",
		ProvinceCount: len(provinces),
	})

	for provinceIndex, province := range provinces {
		cities, err := s.fetchCities(ctx, year, province.Code)
		if err != nil {
			return syncProgress{}, err
		}
		if len(cities) > 0 {
			if err := s.Repo.UpsertCities(ctx, cities); err != nil {
				return syncProgress{}, err
			}
			cityCount += len(cities)
			locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.CityKey(province.Code))
		}

		progress(syncProgress{
			Message:       fmt.Sprintf("Processed province %d/%d: %s", provinceIndex+1, len(provinces), province.Name),
			ProvinceCount: len(provinces),
			CityCount:     cityCount,
			DistrictCount: districtCount,
			VillageCount:  villageCount,
		})

		for cityIndex, city := range cities {
			districts, err := s.fetchDistricts(ctx, year, province.Code, city.Code)
			if err != nil {
				return syncProgress{}, err
			}
			if len(districts) > 0 {
				if err := s.Repo.UpsertDistricts(ctx, districts); err != nil {
					return syncProgress{}, err
				}
				districtCount += len(districts)
				locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.DistrictKey(city.Code))
			}

			for _, district := range districts {
				villages, err := s.fetchVillages(ctx, year, province.Code, city.Code, district.Code)
				if err != nil {
					return syncProgress{}, err
				}
				if len(villages) > 0 {
					if err := s.Repo.UpsertVillages(ctx, villages); err != nil {
						return syncProgress{}, err
					}
					villageCount += len(villages)
					locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.VillageKey(district.Code))
				}
			}

			progress(syncProgress{
				Message:       fmt.Sprintf("Processed city %d/%d in province %s", cityIndex+1, len(cities), province.Name),
				ProvinceCount: len(provinces),
				CityCount:     cityCount,
				DistrictCount: districtCount,
				VillageCount:  villageCount,
			})
		}
	}

	locationcache.DeleteKeys(context.Background(), s.Redis, locationcache.Prefix())
	return syncProgress{
		Message:       "Full location sync completed",
		ProvinceCount: len(provinces),
		CityCount:     cityCount,
		DistrictCount: districtCount,
		VillageCount:  villageCount,
	}, nil
}

func (s *LocationService) fetchProvinces(ctx context.Context, year string) ([]domainlocation.Province, error) {
	_ = year
	dataMap, err := s.fetchLocationMap(ctx, s.locationServiceURL("/api/locations/provinces", nil), "province")
	if err != nil {
		return nil, err
	}

	items := make([]domainlocation.Province, 0, len(dataMap))
	now := time.Now()
	for code, name := range dataMap {
		items = append(items, domainlocation.Province{
			Code:      code,
			Name:      name,
			CreatedAt: now,
		})
	}

	return sortProvinces(items), nil
}

func (s *LocationService) fetchCities(ctx context.Context, year, provinceCode string) ([]domainlocation.City, error) {
	_ = year
	cleanProvinceCode := normalizeCodeSegment(provinceCode)
	dataMap, err := s.fetchLocationMap(ctx, s.locationServiceURL("/api/locations/regencies", map[string]string{
		"province_code": cleanProvinceCode,
		"code_format":   "short",
	}), "city")
	if err != nil {
		return nil, err
	}

	items := make([]domainlocation.City, 0, len(dataMap))
	now := time.Now()
	for code, name := range dataMap {
		items = append(items, domainlocation.City{
			Code:         normalizeChildCode(cleanProvinceCode, code),
			ProvinceCode: cleanProvinceCode,
			Name:         name,
			CreatedAt:    now,
		})
	}

	return sortCities(items), nil
}

func (s *LocationService) fetchDistricts(ctx context.Context, year, provinceCode, cityCode string) ([]domainlocation.District, error) {
	_ = year
	cleanProvinceCode := normalizeCodeSegment(provinceCode)
	cleanCityCode := normalizeCodeSegment(cityCode)
	cityParam := childCodeParam(cleanProvinceCode, cleanCityCode)
	dataMap, err := s.fetchLocationMap(ctx, s.locationServiceURL("/api/locations/districts", map[string]string{
		"province_code": cleanProvinceCode,
		"regency_code":  cityParam,
		"code_format":   "short",
	}), "district")
	if err != nil {
		return nil, err
	}

	items := make([]domainlocation.District, 0, len(dataMap))
	now := time.Now()
	for code, name := range dataMap {
		items = append(items, domainlocation.District{
			Code:      normalizeChildCode(cleanCityCode, code),
			CityCode:  cleanCityCode,
			Name:      name,
			CreatedAt: now,
		})
	}

	return sortDistricts(items), nil
}

func (s *LocationService) fetchVillages(ctx context.Context, year, provinceCode, cityCode, districtCode string) ([]domainlocation.Village, error) {
	_ = year
	cleanProvinceCode := normalizeCodeSegment(provinceCode)
	cleanCityCode := normalizeCodeSegment(cityCode)
	cleanDistrictCode := normalizeCodeSegment(districtCode)
	dataMap, err := s.fetchLocationMap(ctx, s.locationServiceURL("/api/locations/villages", map[string]string{
		"province_code": cleanProvinceCode,
		"regency_code":  childCodeParam(cleanProvinceCode, cleanCityCode),
		"district_code": childCodeParam(cleanCityCode, cleanDistrictCode),
		"code_format":   "short",
	}), "village")
	if err != nil {
		return nil, err
	}

	items := make([]domainlocation.Village, 0, len(dataMap))
	now := time.Now()
	for code, name := range dataMap {
		items = append(items, domainlocation.Village{
			Code:         normalizeChildCode(cleanDistrictCode, code),
			DistrictCode: cleanDistrictCode,
			Name:         name,
			CreatedAt:    now,
		})
	}

	return sortVillages(items), nil
}

func normalizeSyncLevel(level string) string {
	switch level {
	case "", "all":
		return "all"
	case "province", "city", "district", "village":
		return level
	default:
		return level
	}
}

func normalizeSyncYear(year string) string {
	if year != "" {
		return year
	}

	defaultYear := utils.GetEnv("LOCATION_SOURCE_YEAR", "")
	if defaultYear != "" {
		return defaultYear
	}

	return fmt.Sprintf("%d", time.Now().Year())
}

var _ interfacelocation.ServiceLocationInterface = (*LocationService)(nil)
