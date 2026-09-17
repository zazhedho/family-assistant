package servicelocation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"

	"starter-kit/internal/authscope"
	domainlocation "starter-kit/internal/domain/location"
	"starter-kit/internal/dto"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

type locationRepoTestDouble struct {
	provinces []domainlocation.Province
	cities    []domainlocation.City
	districts []domainlocation.District
	villages  []domainlocation.Village
	city      domainlocation.City
	district  domainlocation.District
	listErr   error

	activeJob    domainlocation.SyncJob
	activeJobErr error
	syncJob      domainlocation.SyncJob
	syncJobErr   error
	createdJob   *domainlocation.SyncJob
	updatedJobs  []domainlocation.SyncJob
	failMessage  string

	upsertProvinceCount int
	upsertCityCount     int
	upsertDistrictCount int
	upsertVillageCount  int
	upsertProvinceErr   error
	upsertCityErr       error
	upsertDistrictErr   error
	upsertVillageErr    error
	createSyncJobErr    error
	updateSyncJobErr    error
	failActiveErr       error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (m *locationRepoTestDouble) ListProvinces(ctx context.Context) ([]domainlocation.Province, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]domainlocation.Province{}, m.provinces...), nil
}
func (m *locationRepoTestDouble) ListCitiesByProvince(ctx context.Context, provinceCode string) ([]domainlocation.City, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]domainlocation.City{}, m.cities...), nil
}
func (m *locationRepoTestDouble) ListDistrictsByCity(ctx context.Context, cityCode string) ([]domainlocation.District, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]domainlocation.District{}, m.districts...), nil
}
func (m *locationRepoTestDouble) ListVillagesByDistrict(ctx context.Context, districtCode string) ([]domainlocation.Village, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]domainlocation.Village{}, m.villages...), nil
}
func (m *locationRepoTestDouble) GetProvinceByCode(ctx context.Context, code string) (domainlocation.Province, error) {
	return domainlocation.Province{}, errors.New("not implemented")
}
func (m *locationRepoTestDouble) GetCityByCode(ctx context.Context, code string) (domainlocation.City, error) {
	if m.city.Code != "" {
		return m.city, nil
	}
	for _, city := range m.cities {
		if city.Code == code {
			return city, nil
		}
	}
	return domainlocation.City{}, gorm.ErrRecordNotFound
}
func (m *locationRepoTestDouble) GetDistrictByCode(ctx context.Context, code string) (domainlocation.District, error) {
	if m.district.Code != "" {
		return m.district, nil
	}
	for _, district := range m.districts {
		if district.Code == code {
			return district, nil
		}
	}
	return domainlocation.District{}, gorm.ErrRecordNotFound
}
func (m *locationRepoTestDouble) UpsertProvinces(ctx context.Context, items []domainlocation.Province) error {
	if m.upsertProvinceErr != nil {
		return m.upsertProvinceErr
	}
	m.upsertProvinceCount += len(items)
	return nil
}
func (m *locationRepoTestDouble) UpsertCities(ctx context.Context, items []domainlocation.City) error {
	if m.upsertCityErr != nil {
		return m.upsertCityErr
	}
	m.upsertCityCount += len(items)
	return nil
}
func (m *locationRepoTestDouble) UpsertDistricts(ctx context.Context, items []domainlocation.District) error {
	if m.upsertDistrictErr != nil {
		return m.upsertDistrictErr
	}
	m.upsertDistrictCount += len(items)
	return nil
}
func (m *locationRepoTestDouble) UpsertVillages(ctx context.Context, items []domainlocation.Village) error {
	if m.upsertVillageErr != nil {
		return m.upsertVillageErr
	}
	m.upsertVillageCount += len(items)
	return nil
}
func (m *locationRepoTestDouble) CreateSyncJob(ctx context.Context, job *domainlocation.SyncJob) error {
	if m.createSyncJobErr != nil {
		return m.createSyncJobErr
	}
	m.createdJob = new(*job)
	return nil
}
func (m *locationRepoTestDouble) UpdateSyncJob(ctx context.Context, job *domainlocation.SyncJob) error {
	copyJob := *job
	m.updatedJobs = append(m.updatedJobs, copyJob)
	return m.updateSyncJobErr
}
func (m *locationRepoTestDouble) GetSyncJobByID(ctx context.Context, id string) (domainlocation.SyncJob, error) {
	if m.syncJobErr != nil {
		return domainlocation.SyncJob{}, m.syncJobErr
	}
	return m.syncJob, nil
}
func (m *locationRepoTestDouble) GetActiveSyncJob(ctx context.Context) (domainlocation.SyncJob, error) {
	if m.activeJobErr != nil {
		return domainlocation.SyncJob{}, m.activeJobErr
	}
	return m.activeJob, nil
}
func (m *locationRepoTestDouble) FailActiveSyncJobs(ctx context.Context, message string) error {
	m.failMessage = message
	return m.failActiveErr
}

func TestGetProvinceMapsRepositoryRows(t *testing.T) {
	svc := NewLocationService(&locationRepoTestDouble{
		provinces: []domainlocation.Province{
			{Code: "11", Name: "Aceh"},
			{Code: "12", Name: "Sumatera Utara"},
		},
	})

	got, err := svc.GetProvince(context.Background())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	want := []dto.Location{{Code: "11", Name: "Aceh"}, {Code: "12", Name: "Sumatera Utara"}}
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %+v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: expected %+v, got %+v", i, want[i], got[i])
		}
	}
}

func TestLocationServiceReadMethodsMapRepositoryRows(t *testing.T) {
	now := time.Now()
	svc := NewLocationService(&locationRepoTestDouble{
		cities:    []domainlocation.City{{Code: "3171", Name: "Jakarta Selatan"}},
		districts: []domainlocation.District{{Code: "317101", Name: "Tebet"}},
		villages:  []domainlocation.Village{{Code: "31710101", Name: "Tebet Barat"}},
		syncJob: domainlocation.SyncJob{
			ID:        "job-1",
			Status:    "done",
			Level:     "province",
			Year:      "2026",
			CreatedAt: now,
			UpdatedAt: &now,
		},
	})

	if got, err := svc.GetCity(context.Background(), "31"); err != nil || len(got) != 1 || got[0].Code != "3171" {
		t.Fatalf("get city: got=%+v err=%v", got, err)
	}
	if got, err := svc.GetDistrict(context.Background(), "3171"); err != nil || len(got) != 1 || got[0].Code != "317101" {
		t.Fatalf("get district: got=%+v err=%v", got, err)
	}
	if got, err := svc.GetVillage(context.Background(), "317101"); err != nil || len(got) != 1 || got[0].Code != "31710101" {
		t.Fatalf("get village: got=%+v err=%v", got, err)
	}
	if got, err := svc.GetSyncJob(context.Background(), "job-1"); err != nil || got.ID != "job-1" {
		t.Fatalf("get sync job: got=%+v err=%v", got, err)
	}
}

func TestLocationServiceReadMethodsReturnRepositoryErrors(t *testing.T) {
	svc := NewLocationService(&locationRepoTestDouble{listErr: errors.New("database down")})
	ctx := context.Background()

	if _, err := svc.GetProvince(ctx); err == nil {
		t.Fatal("expected province error")
	}
	if _, err := svc.GetCity(ctx, "31"); err == nil {
		t.Fatal("expected city error")
	}
	if _, err := svc.GetDistrict(ctx, "3171"); err == nil {
		t.Fatal("expected district error")
	}
	if _, err := svc.GetVillage(ctx, "317101"); err == nil {
		t.Fatal("expected village error")
	}
}

func TestLocationServiceReadMethodsFallbackToLocationServiceWhenRepositoryEmpty(t *testing.T) {
	repo := &locationRepoTestDouble{
		city:     domainlocation.City{Code: "1671", ProvinceCode: "16", Name: "Palembang"},
		district: domainlocation.District{Code: "167101", CityCode: "1671", Name: "Ilir Timur I"},
	}
	svc := &LocationService{
		Repo: repo,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{}`
			switch {
			case strings.Contains(req.URL.Path, "/api/locations/provinces"):
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"16","name":"Sumatera Selatan"}]}`
			case strings.Contains(req.URL.Path, "/api/locations/regencies"):
				if got := req.URL.Query().Get("province_code"); got != "16" {
					t.Fatalf("expected province_code 16, got %q", got)
				}
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"71","name":"Kota Palembang"}]}`
			case strings.Contains(req.URL.Path, "/api/locations/districts"):
				if got := req.URL.Query().Get("regency_code"); got != "71" {
					t.Fatalf("expected regency_code 71, got %q", got)
				}
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"01","name":"Ilir Timur I"}]}`
			case strings.Contains(req.URL.Path, "/api/locations/villages"):
				if got := req.URL.Query().Get("district_code"); got != "01" {
					t.Fatalf("expected district_code 01, got %q", got)
				}
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"1001","name":"Sungai Pangeran"}]}`
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})},
	}
	ctx := context.Background()

	provinces, err := svc.GetProvince(ctx)
	if err != nil || len(provinces) != 1 || provinces[0].Code != "16" {
		t.Fatalf("province fallback failed: rows=%+v err=%v", provinces, err)
	}
	cities, err := svc.GetCity(ctx, "16")
	if err != nil || len(cities) != 1 || cities[0].Code != "1671" {
		t.Fatalf("city fallback failed: rows=%+v err=%v", cities, err)
	}
	districts, err := svc.GetDistrict(ctx, "1671")
	if err != nil || len(districts) != 1 || districts[0].Code != "167101" {
		t.Fatalf("district fallback failed: rows=%+v err=%v", districts, err)
	}
	villages, err := svc.GetVillage(ctx, "167101")
	if err != nil || len(villages) != 1 || villages[0].Code != "1671011001" {
		t.Fatalf("village fallback failed: rows=%+v err=%v", villages, err)
	}
}

func TestStartSyncRejectsMissingScopedCodes(t *testing.T) {
	svc := NewLocationService(&locationRepoTestDouble{activeJobErr: gorm.ErrRecordNotFound})

	_, err := svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "city"})
	if err == nil || err.Error() != "province_code is required for city sync" {
		t.Fatalf("expected city sync validation error, got %v", err)
	}

	_, err = svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "district", ProvinceCode: "31"})
	if err == nil || err.Error() != "province_code and city_code are required for district sync" {
		t.Fatalf("expected district sync validation error, got %v", err)
	}

	_, err = svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "village", ProvinceCode: "31", CityCode: "3171"})
	if err == nil || err.Error() != "province_code, city_code, and district_code are required for village sync" {
		t.Fatalf("expected village sync validation error, got %v", err)
	}

	_, err = svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "unknown"})
	if err == nil || err.Error() != "invalid sync level" {
		t.Fatalf("expected invalid level error, got %v", err)
	}
}

func TestStartSyncReturnsActiveJobWhenAlreadyRunning(t *testing.T) {
	now := time.Now()
	svc := NewLocationService(&locationRepoTestDouble{
		activeJob: domainlocation.SyncJob{
			ID:        "job-1",
			Status:    "running",
			Level:     "all",
			Year:      "2025",
			Message:   "running",
			CreatedAt: now,
			UpdatedAt: &now,
		},
	})

	got, err := svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "all", Year: "2025"})
	if !errors.Is(err, ErrLocationSyncRunning) {
		t.Fatalf("expected ErrLocationSyncRunning, got %v", err)
	}
	if got.ID != "job-1" || got.Status != "running" {
		t.Fatalf("expected active job response, got %+v", got)
	}
}

func TestStartSyncCreatesQueuedJobWithDefaults(t *testing.T) {
	t.Setenv("LOCATION_SOURCE_YEAR", "2026")
	repo := &locationRepoTestDouble{activeJobErr: gorm.ErrRecordNotFound}
	svc := NewLocationService(repo)

	got, err := svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "province"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got.Status != "queued" || got.Level != "province" || got.Year != "2026" {
		t.Fatalf("unexpected job response: %+v", got)
	}
	if repo.createdJob == nil {
		t.Fatal("expected sync job to be created")
	}
	if repo.createdJob.RequestedBy != "user-1" {
		t.Fatalf("expected requested user id, got %+v", repo.createdJob)
	}
}

func TestStartSyncRepositoryErrors(t *testing.T) {
	svc := NewLocationService(&locationRepoTestDouble{activeJobErr: errors.New("database down")})
	if _, err := svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "all", Year: "2026"}); err == nil {
		t.Fatal("expected active job lookup error")
	}

	svc = NewLocationService(&locationRepoTestDouble{activeJobErr: gorm.ErrRecordNotFound, createSyncJobErr: errors.New("database down")})
	if _, err := svc.StartSync(locationTestActorContext("user-1"), dto.SyncLocationRequest{Level: "all", Year: "2026"}); err == nil {
		t.Fatal("expected create sync job error")
	}
}

func TestLocationHelperCacheKeysAndCodeNormalization(t *testing.T) {
	if got := normalizeChildCode("11", "01"); got != "1101" {
		t.Fatalf("expected normalized child code, got %q", got)
	}
	if got := normalizeChildCode("11", "1101"); got != "1101" {
		t.Fatalf("expected existing parent prefix to be preserved, got %q", got)
	}
	wantCandidates := []string{"01", "1101"}
	if got := childCodeCandidates("11", "1101"); !reflect.DeepEqual(got, wantCandidates) {
		t.Fatalf("expected %v, got %v", wantCandidates, got)
	}
}

func locationTestActorContext(userID string) context.Context {
	return authscope.WithContext(context.Background(), authscope.New(userID, "admin", "admin", nil))
}

func TestLocationMapAndSortHelpers(t *testing.T) {
	provinces := sortProvinces([]domainlocation.Province{{Code: "12", Name: "Zulu"}, {Code: "11", Name: "Aceh"}})
	if provinces[0].Name != "Aceh" {
		t.Fatalf("expected province sort by name, got %+v", provinces)
	}
	cities := sortCities([]domainlocation.City{{Code: "2", Name: "Zulu"}, {Code: "1", Name: "Aceh"}})
	if cities[0].Name != "Aceh" {
		t.Fatalf("expected city sort by name, got %+v", cities)
	}
	districts := sortDistricts([]domainlocation.District{{Code: "2", Name: "Zulu"}, {Code: "1", Name: "Aceh"}})
	if districts[0].Name != "Aceh" {
		t.Fatalf("expected district sort by name, got %+v", districts)
	}
	villages := sortVillages([]domainlocation.Village{{Code: "2", Name: "Zulu"}, {Code: "1", Name: "Aceh"}})
	if villages[0].Name != "Aceh" {
		t.Fatalf("expected village sort by name, got %+v", villages)
	}
	if got := mapCities([]domainlocation.City{{Code: "1101", Name: "Banda Aceh"}}); len(got) != 1 || got[0].Code != "1101" {
		t.Fatalf("unexpected city mapping: %+v", got)
	}
	if got := mapDistricts([]domainlocation.District{{Code: "110101", Name: "Kuta Alam"}}); len(got) != 1 || got[0].Name != "Kuta Alam" {
		t.Fatalf("unexpected district mapping: %+v", got)
	}
	if got := mapVillages([]domainlocation.Village{{Code: "11010101", Name: "Village"}}); len(got) != 1 || got[0].Code != "11010101" {
		t.Fatalf("unexpected village mapping: %+v", got)
	}
}

func TestFetchLocationMapHandlesHTTPResponses(t *testing.T) {
	svc := &LocationService{HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"11","full_code":"11","name":"Aceh","level":"province"}]}`)),
			Header:     make(http.Header),
		}, nil
	})}}
	got, err := svc.fetchLocationMap(context.Background(), "https://example.com/location", "province")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got["11"] != "Aceh" {
		t.Fatalf("unexpected location map: %+v", got)
	}

	svc.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := svc.fetchLocationMap(context.Background(), "https://example.com/location", "province"); err == nil {
		t.Fatal("expected status error")
	}

	svc.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	if _, err := svc.fetchLocationMap(context.Background(), "https://example.com/location", "province"); err == nil {
		t.Fatal("expected transport error")
	}

	svc.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{`)),
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := svc.fetchLocationMap(context.Background(), "https://example.com/location", "province"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestLocationFetchScopedLevelsAndSyncAll(t *testing.T) {
	repo := &locationRepoTestDouble{}
	svc := &LocationService{
		Repo: repo,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{}`
			switch {
			case strings.Contains(req.URL.Path, "/api/locations/provinces"):
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"31","full_code":"31","name":"DKI Jakarta","level":"province"}]}`
			case strings.Contains(req.URL.Path, "/api/locations/regencies"):
				if got := req.URL.Query().Get("province_code"); got != "31" {
					t.Fatalf("expected province_code 31, got %q", got)
				}
				if got := req.URL.Query().Get("code_format"); got != "short" {
					t.Fatalf("expected short code format, got %q", got)
				}
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"71","full_code":"31.71","name":"Jakarta Selatan","level":"regency","parent_code":"31"}]}`
			case strings.Contains(req.URL.Path, "/api/locations/districts"):
				if got := req.URL.Query().Get("regency_code"); got != "71" {
					t.Fatalf("expected regency_code 71, got %q", got)
				}
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"01","full_code":"31.71.01","name":"Tebet","level":"district","parent_code":"31.71"}]}`
			case strings.Contains(req.URL.Path, "/api/locations/villages"):
				if got := req.URL.Query().Get("district_code"); got != "01" {
					t.Fatalf("expected district_code 01, got %q", got)
				}
				body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"01","full_code":"31.71.01.0001","name":"Tebet Barat","level":"village","parent_code":"31.71.01"}]}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})},
	}
	ctx := context.Background()

	cities, err := svc.fetchCities(ctx, "2026", "31")
	if err != nil || len(cities) != 1 || cities[0].Code != "3171" {
		t.Fatalf("fetch cities: cities=%+v err=%v", cities, err)
	}
	districts, err := svc.fetchDistricts(ctx, "2026", "31", "3171")
	if err != nil || len(districts) != 1 || districts[0].Code != "317101" {
		t.Fatalf("fetch districts: districts=%+v err=%v", districts, err)
	}
	villages, err := svc.fetchVillages(ctx, "2026", "31", "3171", "317101")
	if err != nil || len(villages) != 1 || villages[0].Code != "31710101" {
		t.Fatalf("fetch villages: villages=%+v err=%v", villages, err)
	}

	var progressCalls int
	result, err := svc.sync(ctx, dto.SyncLocationRequest{Level: "all", Year: "2026"}, func(progress syncProgress) {
		progressCalls++
	})
	if err != nil {
		t.Fatalf("sync all: %v", err)
	}
	if result.ProvinceCount != 1 || result.CityCount != 1 || result.DistrictCount != 1 || result.VillageCount != 1 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if progressCalls == 0 {
		t.Fatal("expected progress callbacks")
	}
	if repo.upsertProvinceCount != 1 || repo.upsertCityCount != 1 || repo.upsertDistrictCount != 1 || repo.upsertVillageCount != 1 {
		t.Fatalf("expected all upserts, repo=%+v", repo)
	}
}

func TestLocationSyncIndividualLevels(t *testing.T) {
	makeService := func(repo *locationRepoTestDouble) *LocationService {
		return &LocationService{
			Repo: repo,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := `{}`
				switch {
				case strings.Contains(req.URL.Path, "/api/locations/provinces"):
					body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"31","name":"DKI Jakarta"}]}`
				case strings.Contains(req.URL.Path, "/api/locations/regencies"):
					body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"71","name":"Jakarta Selatan"}]}`
				case strings.Contains(req.URL.Path, "/api/locations/districts"):
					body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"01","name":"Tebet"}]}`
				case strings.Contains(req.URL.Path, "/api/locations/villages"):
					body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"01","name":"Tebet Barat"}]}`
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			})},
		}
	}

	tests := []dto.SyncLocationRequest{
		{Level: "province", Year: "2026"},
		{Level: "city", Year: "2026", ProvinceCode: "31"},
		{Level: "district", Year: "2026", ProvinceCode: "31", CityCode: "3171"},
		{Level: "village", Year: "2026", ProvinceCode: "31", CityCode: "3171", DistrictCode: "317101"},
	}

	for _, req := range tests {
		t.Run(req.Level, func(t *testing.T) {
			repo := &locationRepoTestDouble{}
			svc := makeService(repo)
			result, err := svc.sync(context.Background(), req, func(progress syncProgress) {})
			if err != nil {
				t.Fatalf("expected sync success, got %v", err)
			}
			if result.Message == "" {
				t.Fatalf("expected result message, got %+v", result)
			}
		})
	}

	svc := makeService(&locationRepoTestDouble{})
	if _, err := svc.sync(context.Background(), dto.SyncLocationRequest{Level: "bad"}, func(progress syncProgress) {}); err == nil {
		t.Fatal("expected invalid sync level error")
	}
}

func TestLocationSyncUpsertErrors(t *testing.T) {
	makeService := func(repo *locationRepoTestDouble) *LocationService {
		return &LocationService{
			Repo: repo,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"31","name":"DKI Jakarta"}]}`
				if strings.Contains(req.URL.Path, "/api/locations/regencies") {
					body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"71","name":"Jakarta Selatan"}]}`
				}
				if strings.Contains(req.URL.Path, "/api/locations/districts") {
					body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"01","name":"Tebet"}]}`
				}
				if strings.Contains(req.URL.Path, "/api/locations/villages") {
					body = `{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"01","name":"Tebet Barat"}]}`
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})},
		}
	}

	tests := []struct {
		name string
		repo *locationRepoTestDouble
		req  dto.SyncLocationRequest
	}{
		{name: "province", repo: &locationRepoTestDouble{upsertProvinceErr: errors.New("database down")}, req: dto.SyncLocationRequest{Level: "province", Year: "2026"}},
		{name: "city", repo: &locationRepoTestDouble{upsertCityErr: errors.New("database down")}, req: dto.SyncLocationRequest{Level: "city", Year: "2026", ProvinceCode: "31"}},
		{name: "district", repo: &locationRepoTestDouble{upsertDistrictErr: errors.New("database down")}, req: dto.SyncLocationRequest{Level: "district", Year: "2026", ProvinceCode: "31", CityCode: "3171"}},
		{name: "village", repo: &locationRepoTestDouble{upsertVillageErr: errors.New("database down")}, req: dto.SyncLocationRequest{Level: "village", Year: "2026", ProvinceCode: "31", CityCode: "3171", DistrictCode: "317101"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := makeService(tt.repo)
			if _, err := svc.sync(context.Background(), tt.req, func(progress syncProgress) {}); err == nil {
				t.Fatal("expected upsert error")
			}
		})
	}
}

func TestRunSyncJobCompletesAndHandlesGuards(t *testing.T) {
	now := time.Now()
	repo := &locationRepoTestDouble{syncJob: domainlocation.SyncJob{ID: "job-1", Status: "queued", Level: "province", Year: "2026", CreatedAt: now}}
	svc := &LocationService{
		Repo: repo,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"log_id":"test","code":200,"status":true,"message":"Success","data":[{"code":"31","name":"DKI Jakarta"}]}`)), Header: make(http.Header)}, nil
		})},
	}

	svc.runSyncJob("job-1", dto.SyncLocationRequest{Level: "province", Year: "2026"})
	if len(repo.updatedJobs) < 2 {
		t.Fatalf("expected running and completed job updates, got %+v", repo.updatedJobs)
	}
	if got := repo.updatedJobs[len(repo.updatedJobs)-1]; got.Status != "completed" || got.ProvinceCount != 1 {
		t.Fatalf("expected completed job, got %+v", got)
	}

	repo = &locationRepoTestDouble{syncJob: domainlocation.SyncJob{ID: "job-2", Status: "queued", CreatedAt: now}}
	svc = &LocationService{Repo: repo}
	svc.syncing.Store(true)
	svc.runSyncJob("job-2", dto.SyncLocationRequest{Level: "province", Year: "2026"})
	if len(repo.updatedJobs) != 1 || repo.updatedJobs[0].Status != "failed" {
		t.Fatalf("expected busy sync to mark failed, got %+v", repo.updatedJobs)
	}
}

func TestLocationSyncProgressAndFailureHelpers(t *testing.T) {
	now := time.Now()
	repo := &locationRepoTestDouble{syncJob: domainlocation.SyncJob{ID: "job-1", Status: "running", CreatedAt: now}}
	svc := &LocationService{Repo: repo}

	job := domainlocation.SyncJob{ID: "job-1"}
	svc.applySyncProgress(&job, syncProgress{Message: "halfway", ProvinceCount: 1})
	if job.Message != "halfway" || job.ProvinceCount != 1 || job.UpdatedAt == nil {
		t.Fatalf("unexpected progress application: %+v", job)
	}

	svc.markSyncJobFailed(context.Background(), "job-1", "failed")
	if repo.syncJob.Status != "running" {
		t.Fatalf("test double should keep original sync job copy, got %+v", repo.syncJob)
	}
}
