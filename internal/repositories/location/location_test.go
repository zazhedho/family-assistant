package repositorylocation

import (
	"context"
	domainlocation "starter-kit/internal/domain/location"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		DryRun:                 true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db
}

func TestLocationRepositoryDryRunReadsAndWrites(t *testing.T) {
	repo := NewLocationRepo(newDryRunDB(t))
	ctx := context.Background()

	if _, err := repo.ListProvinces(ctx); err != nil {
		t.Fatalf("list provinces: %v", err)
	}
	if _, err := repo.ListCitiesByProvince(ctx, "31"); err != nil {
		t.Fatalf("list cities: %v", err)
	}
	if _, err := repo.ListDistrictsByCity(ctx, "3171"); err != nil {
		t.Fatalf("list districts: %v", err)
	}
	if _, err := repo.ListVillagesByDistrict(ctx, "317101"); err != nil {
		t.Fatalf("list villages: %v", err)
	}
	if _, err := repo.GetProvinceByCode(ctx, "31"); err != nil {
		t.Fatalf("get province: %v", err)
	}
	if _, err := repo.GetCityByCode(ctx, "3171"); err != nil {
		t.Fatalf("get city: %v", err)
	}
	if _, err := repo.GetDistrictByCode(ctx, "317101"); err != nil {
		t.Fatalf("get district: %v", err)
	}

	if err := repo.UpsertProvinces(ctx, []domainlocation.Province{{Code: "31", Name: "DKI Jakarta"}}); err != nil {
		t.Fatalf("upsert provinces: %v", err)
	}
	if err := repo.UpsertCities(ctx, []domainlocation.City{{Code: "3171", ProvinceCode: "31", Name: "Jakarta Selatan"}}); err != nil {
		t.Fatalf("upsert cities: %v", err)
	}
	if err := repo.UpsertDistricts(ctx, []domainlocation.District{{Code: "317101", CityCode: "3171", Name: "Tebet"}}); err != nil {
		t.Fatalf("upsert districts: %v", err)
	}
	if err := repo.UpsertVillages(ctx, []domainlocation.Village{{Code: "31710101", DistrictCode: "317101", Name: "Tebet Barat"}}); err != nil {
		t.Fatalf("upsert villages: %v", err)
	}

	job := &domainlocation.SyncJob{ID: "job-1", Status: "queued"}
	if err := repo.CreateSyncJob(ctx, job); err != nil {
		t.Fatalf("create sync job: %v", err)
	}
	if err := repo.UpdateSyncJob(ctx, job); err != nil {
		t.Fatalf("update sync job: %v", err)
	}
	if _, err := repo.GetSyncJobByID(ctx, "job-1"); err != nil {
		t.Fatalf("get sync job: %v", err)
	}
	if _, err := repo.GetActiveSyncJob(ctx); err != nil {
		t.Fatalf("get active sync job: %v", err)
	}
	if err := repo.FailActiveSyncJobs(ctx, "stopped"); err != nil {
		t.Fatalf("fail active sync jobs: %v", err)
	}
}

func TestLocationRepositoryUpsertIgnoresUnknownType(t *testing.T) {
	repo := NewLocationRepo(newDryRunDB(t)).(*repo)
	if err := repo.upsert(context.Background(), "code", []string{"unsupported"}); err != nil {
		t.Fatalf("unsupported upsert should be ignored: %v", err)
	}
}
