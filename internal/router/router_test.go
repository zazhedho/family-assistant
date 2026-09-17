package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNewRoutesRegistersHealthcheck(t *testing.T) {
	routes := NewRoutes()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthcheck", nil)
	routes.App.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSessionRoutesSkipsWhenRedisUnavailable(t *testing.T) {
	routes := NewRoutes()
	routes.SessionRoutes()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/user/sessions", nil)
	routes.App.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected unregistered route to return 404, got %d", rec.Code)
	}
}

func newRouterDryRunDB(t *testing.T) *gorm.DB {
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

func TestRouteGroupsRegisterWithDryRunDB(t *testing.T) {
	t.Setenv("MEDIA_ENABLED", "false")
	routes := NewRoutes()
	routes.DB = newRouterDryRunDB(t)

	routes.UserRoutes()
	routes.RoleRoutes()
	routes.PermissionRoutes()
	routes.MenuRoutes()
	routes.AppConfigRoutes()
	routes.AuditRoutes()
	routes.LocationRoutes()
	if err := routes.MediaRoutes(); err != nil {
		t.Fatalf("register media routes: %v", err)
	}

	registered := map[string]bool{}
	for _, route := range routes.App.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, want := range []string{
		"POST /api/user/register",
		"POST /api/user/login",
		"GET /api/roles",
		"GET /api/permissions",
		"GET /api/menus",
		"GET /api/configs",
		"GET /api/audits",
		"GET /api/location/province",
		"POST /api/location/sync",
	} {
		if !registered[want] {
			t.Fatalf("expected route %s to be registered", want)
		}
	}
}
