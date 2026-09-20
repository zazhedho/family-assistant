package main

import (
	"testing"

	"family-assistant/internal/router"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFailOnErrorWithNilError(t *testing.T) {
	FailOnError(nil, "should not fail")
}

func TestRegisterRoutesWiresSharedSpaceEndpoints(t *testing.T) {
	t.Setenv("MEDIA_ENABLED", "false")
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DryRun: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}

	routes := router.NewRoutes()
	routes.DB = db
	if err := registerRoutes(routes); err != nil {
		t.Fatalf("register routes: %v", err)
	}
	for _, want := range []string{"GET /api/spaces", "POST /api/spaces", "GET /api/spaces/:space_id/members"} {
		found := false
		for _, route := range routes.App.Routes() {
			if route.Method+" "+route.Path == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("startup registration missing %s", want)
		}
	}
}
