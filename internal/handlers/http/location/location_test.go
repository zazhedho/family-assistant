package handlerlocation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"starter-kit/internal/authscope"
	"starter-kit/internal/dto"
	servicelocation "starter-kit/internal/services/location"
	"starter-kit/utils"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type locationServiceTestDouble struct {
	locations []dto.Location
	job       dto.LocationSyncJob
	err       error
	startReq  dto.SyncLocationRequest
	requested string
}

func (m *locationServiceTestDouble) GetProvince(ctx context.Context) ([]dto.Location, error) {
	return m.locations, m.err
}
func (m *locationServiceTestDouble) GetCity(ctx context.Context, provinceCode string) ([]dto.Location, error) {
	return m.locations, m.err
}
func (m *locationServiceTestDouble) GetDistrict(ctx context.Context, cityCode string) ([]dto.Location, error) {
	return m.locations, m.err
}
func (m *locationServiceTestDouble) GetVillage(ctx context.Context, districtCode string) ([]dto.Location, error) {
	return m.locations, m.err
}
func (m *locationServiceTestDouble) StartSync(ctx context.Context, req dto.SyncLocationRequest) (dto.LocationSyncJob, error) {
	m.startReq = req
	m.requested = authscope.FromContext(ctx).ActorUserID()
	return m.job, m.err
}
func (m *locationServiceTestDouble) GetSyncJob(ctx context.Context, id string) (dto.LocationSyncJob, error) {
	return m.job, m.err
}

func performLocationRequest(method, routePath, requestPath string, body any, handler gin.HandlerFunc, authData map[string]any) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, func(ctx *gin.Context) {
		if authData != nil {
			ctx.Set(utils.CtxKeyAuthData, authData)
			ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), authscope.NewFromClaims(authData, nil)))
		}
		handler(ctx)
	})
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, requestPath, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestLocationListHandlersValidateRequiredQueries(t *testing.T) {
	handler := NewLocationHandler(&locationServiceTestDouble{locations: []dto.Location{{Code: "11", Name: "Aceh"}}})

	if rec := performLocationRequest(http.MethodGet, "/province", "/province", nil, handler.GetProvince, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected province 200, got %d", rec.Code)
	}
	if rec := performLocationRequest(http.MethodGet, "/city", "/city", nil, handler.GetCity, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected city 400, got %d", rec.Code)
	}
	if rec := performLocationRequest(http.MethodGet, "/district", "/district", nil, handler.GetDistrict, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected district 400, got %d", rec.Code)
	}
	if rec := performLocationRequest(http.MethodGet, "/village", "/village", nil, handler.GetVillage, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected village 400, got %d", rec.Code)
	}

	if rec := performLocationRequest(http.MethodGet, "/city", "/city?province_code=11", nil, handler.GetCity, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected city 200, got %d", rec.Code)
	}
	if rec := performLocationRequest(http.MethodGet, "/district", "/district?city_code=1101", nil, handler.GetDistrict, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected district 200, got %d", rec.Code)
	}
	if rec := performLocationRequest(http.MethodGet, "/village", "/village?district_code=110101", nil, handler.GetVillage, nil); rec.Code != http.StatusOK {
		t.Fatalf("expected village 200, got %d", rec.Code)
	}
}

func TestLocationListHandlersMapServiceErrors(t *testing.T) {
	handler := NewLocationHandler(&locationServiceTestDouble{err: errors.New("database down")})

	tests := []struct {
		name      string
		routePath string
		path      string
		call      gin.HandlerFunc
	}{
		{name: "province", routePath: "/province", path: "/province", call: handler.GetProvince},
		{name: "city", routePath: "/city", path: "/city?province_code=11", call: handler.GetCity},
		{name: "district", routePath: "/district", path: "/district?city_code=1101", call: handler.GetDistrict},
		{name: "village", routePath: "/village", path: "/village?district_code=110101", call: handler.GetVillage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := performLocationRequest(http.MethodGet, tt.routePath, tt.path, nil, tt.call, nil)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLocationSyncMapsAcceptedConflictAndBadRequest(t *testing.T) {
	service := &locationServiceTestDouble{job: dto.LocationSyncJob{ID: "job-1", Status: "queued"}}
	handler := NewLocationHandler(service)
	rec := performLocationRequest(http.MethodPost, "/sync", "/sync", dto.SyncLocationRequest{Level: "province"}, handler.Sync, map[string]any{
		"user_id": "user-1",
		"role":    "admin",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.requested != "user-1" {
		t.Fatalf("expected actor user id, got %q", service.requested)
	}

	handler = NewLocationHandler(&locationServiceTestDouble{err: servicelocation.ErrLocationSyncRunning, job: dto.LocationSyncJob{ID: "job-1"}})
	rec = performLocationRequest(http.MethodPost, "/sync", "/sync", dto.SyncLocationRequest{Level: "province"}, handler.Sync, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}

	handler = NewLocationHandler(&locationServiceTestDouble{err: errors.New("invalid sync level")})
	rec = performLocationRequest(http.MethodPost, "/sync", "/sync", dto.SyncLocationRequest{Level: "province"}, handler.Sync, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	rec = performLocationRequest(http.MethodPost, "/sync", "/sync", map[string]any{"level": 123}, handler.Sync, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid json 400, got %d", rec.Code)
	}
}

func TestLocationGetSyncJobMapsNotFound(t *testing.T) {
	handler := NewLocationHandler(&locationServiceTestDouble{err: gorm.ErrRecordNotFound})
	rec := performLocationRequest(http.MethodGet, "/sync/:id", "/sync/job-1", nil, handler.GetSyncJob, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewLocationHandler(&locationServiceTestDouble{job: dto.LocationSyncJob{ID: "job-1", Status: "done"}})
	rec = performLocationRequest(http.MethodGet, "/sync/:id", "/sync/job-1", nil, handler.GetSyncJob, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewLocationHandler(&locationServiceTestDouble{err: errors.New("database down")})
	rec = performLocationRequest(http.MethodGet, "/sync/:id", "/sync/job-1", nil, handler.GetSyncJob, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = performLocationRequest(http.MethodGet, "/sync", "/sync", nil, handler.GetSyncJob, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing id 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
