package filter

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetBaseParamsAppliesDefaultsAndParsesFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/items?page=2&limit=25&order_by=name&order_direction=DESC&filters[role]=\"admin\"&filters[ids]=[\"1\",\"2\"]", nil)

	got, err := GetBaseParams(ctx, "created_at", "desc", 10)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got.Page != 2 || got.Limit != 25 || got.Offset != 25 {
		t.Fatalf("unexpected pagination: %+v", got)
	}
	if got.OrderBy != "name" || got.OrderDirection != "DESC" {
		t.Fatalf("unexpected ordering: %+v", got)
	}
	if got.Filters["role"] != "admin" {
		t.Fatalf("expected role filter, got %#v", got.Filters["role"])
	}
	ids, ok := got.Filters["ids"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("expected parsed ids filter, got %#v", got.Filters["ids"])
	}
}

func TestGetBaseParamsClampsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/items?page=0&limit=20000&order_direction=sideways", nil)

	got, err := GetBaseParams(ctx, "created_at", "desc", 50)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got.Page != 1 || got.Limit != 50 || got.Offset != 0 {
		t.Fatalf("unexpected pagination defaults: %+v", got)
	}
	if got.OrderBy != "created_at" || got.OrderDirection != "desc" {
		t.Fatalf("unexpected order defaults: %+v", got)
	}
}

func TestWhitelistFilterKeepsAllowedKeysOnly(t *testing.T) {
	got := WhitelistFilter(map[string]any{
		"role":  "admin",
		"email": "admin@example.com",
	}, []string{"role"})

	want := map[string]any{"role": "admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestWhitelistStringFilterConvertsValues(t *testing.T) {
	got := WhitelistStringFilter(map[string]any{
		"id":    10,
		"roles": []string{"admin", "viewer"},
		"skip":  true,
	}, []string{"id", "roles"})

	if got["id"] != "10" {
		t.Fatalf("expected string id, got %#v", got["id"])
	}
	if got["roles"] != "admin,viewer" {
		t.Fatalf("expected joined roles, got %#v", got["roles"])
	}
	if _, ok := got["skip"]; ok {
		t.Fatal("expected disallowed key to be removed")
	}
}
