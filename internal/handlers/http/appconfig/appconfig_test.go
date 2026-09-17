package handlerappconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	domainappconfig "starter-kit/internal/domain/appconfig"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type appConfigServiceTestDouble struct {
	configs    []domainappconfig.AppConfig
	config     domainappconfig.AppConfig
	total      int64
	updateReq  dto.UpdateAppConfig
	err        error
	getByIDErr error
	updateErr  error
}

func (m *appConfigServiceTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]domainappconfig.AppConfig, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.configs, m.total, nil
}
func (m *appConfigServiceTestDouble) GetByID(ctx context.Context, id string) (domainappconfig.AppConfig, error) {
	if m.getByIDErr != nil {
		return domainappconfig.AppConfig{}, m.getByIDErr
	}
	if m.err != nil {
		return domainappconfig.AppConfig{}, m.err
	}
	return m.config, nil
}
func (m *appConfigServiceTestDouble) GetByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, error) {
	return m.config, nil
}
func (m *appConfigServiceTestDouble) Update(ctx context.Context, id string, req dto.UpdateAppConfig) (domainappconfig.AppConfig, error) {
	m.updateReq = req
	if m.updateErr != nil {
		return domainappconfig.AppConfig{}, m.updateErr
	}
	if m.err != nil {
		return domainappconfig.AppConfig{}, m.err
	}
	return m.config, nil
}
func (m *appConfigServiceTestDouble) GetString(ctx context.Context, configKey string, fallback string) (string, error) {
	return fallback, nil
}
func (m *appConfigServiceTestDouble) GetBool(ctx context.Context, configKey string, fallback bool) (bool, error) {
	return fallback, nil
}
func (m *appConfigServiceTestDouble) GetInt(ctx context.Context, configKey string, fallback int) (int, error) {
	return fallback, nil
}
func (m *appConfigServiceTestDouble) GetDuration(ctx context.Context, configKey string, fallback time.Duration) (time.Duration, error) {
	return fallback, nil
}
func (m *appConfigServiceTestDouble) DecodeJSON(ctx context.Context, configKey string, target any) error {
	return nil
}
func (m *appConfigServiceTestDouble) IsEnabled(ctx context.Context, configKey string, fallback bool) (bool, error) {
	return fallback, nil
}

type auditServiceAppConfigTestDouble struct {
	events []domainaudit.AuditEvent
}

func (m *auditServiceAppConfigTestDouble) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	m.events = append(m.events, req)
	return nil
}
func (m *auditServiceAppConfigTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}
func (m *auditServiceAppConfigTestDouble) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func performAppConfigRequest(method, routePath, requestPath string, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, handler)

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

func TestGetAllAppConfigsReturnsPagination(t *testing.T) {
	handler := NewAppConfigHandler(&appConfigServiceTestDouble{
		configs: []domainappconfig.AppConfig{{Id: "config-1", ConfigKey: "auth.enabled", Category: "auth"}},
		total:   1,
	}, &auditServiceAppConfigTestDouble{})

	rec := performAppConfigRequest(http.MethodGet, "/configs", "/configs?page=1&limit=10&filters[category]=\"auth\"&filters[ignored]=\"x\"", nil, handler.GetAll)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if decoded["total_data"].(float64) != 1 {
		t.Fatalf("expected total_data 1, got %+v", decoded)
	}
}

func TestGetAppConfigByIDRejectsInvalidUUID(t *testing.T) {
	handler := NewAppConfigHandler(&appConfigServiceTestDouble{}, &auditServiceAppConfigTestDouble{})

	rec := performAppConfigRequest(http.MethodGet, "/configs/:id", "/configs/not-a-uuid", nil, handler.GetByID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetAppConfigByIDReturnsNotFound(t *testing.T) {
	handler := NewAppConfigHandler(&appConfigServiceTestDouble{err: gorm.ErrRecordNotFound}, &auditServiceAppConfigTestDouble{})

	rec := performAppConfigRequest(http.MethodGet, "/configs/:id", "/configs/550e8400-e29b-41d4-a716-446655440000", nil, handler.GetByID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetAllAppConfigsErrorBranches(t *testing.T) {
	handler := NewAppConfigHandler(&appConfigServiceTestDouble{}, &auditServiceAppConfigTestDouble{})
	rec := performAppConfigRequest(http.MethodGet, "/configs", "/configs?page=bad", nil, handler.GetAll)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewAppConfigHandler(&appConfigServiceTestDouble{err: errors.New("database down")}, &auditServiceAppConfigTestDouble{})
	rec = performAppConfigRequest(http.MethodGet, "/configs", "/configs", nil, handler.GetAll)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetAppConfigByIDReturnsOK(t *testing.T) {
	handler := NewAppConfigHandler(&appConfigServiceTestDouble{config: domainappconfig.AppConfig{
		Id:        "550e8400-e29b-41d4-a716-446655440000",
		ConfigKey: "auth.enabled",
		Value:     "true",
	}}, &auditServiceAppConfigTestDouble{})

	rec := performAppConfigRequest(http.MethodGet, "/configs/:id", "/configs/550e8400-e29b-41d4-a716-446655440000", nil, handler.GetByID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAppConfigReturnsOKAndWritesAudit(t *testing.T) {
	auditSvc := &auditServiceAppConfigTestDouble{}
	service := &appConfigServiceTestDouble{config: domainappconfig.AppConfig{
		Id:        "550e8400-e29b-41d4-a716-446655440000",
		ConfigKey: "auth.enabled",
		Value:     "true",
		IsActive:  true,
	}}
	handler := NewAppConfigHandler(service, auditSvc)

	rec := performAppConfigRequest(http.MethodPut, "/configs/:id", "/configs/550e8400-e29b-41d4-a716-446655440000", dto.UpdateAppConfig{
		Value:    "true",
		IsActive: new(true),
	}, handler.Update)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.updateReq.Value != "true" {
		t.Fatalf("expected update request, got %+v", service.updateReq)
	}
	if len(auditSvc.events) != 1 || auditSvc.events[0].Status != domainaudit.StatusSuccess {
		t.Fatalf("expected success audit event, got %+v", auditSvc.events)
	}
}

func TestUpdateAppConfigMapsServiceErrors(t *testing.T) {
	handler := NewAppConfigHandler(&appConfigServiceTestDouble{err: errors.New("database down")}, &auditServiceAppConfigTestDouble{})

	rec := performAppConfigRequest(http.MethodPut, "/configs/:id", "/configs/550e8400-e29b-41d4-a716-446655440000", dto.UpdateAppConfig{
		Value: "true",
	}, handler.Update)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAppConfigRejectsBadInputAndNotFound(t *testing.T) {
	handler := NewAppConfigHandler(&appConfigServiceTestDouble{}, &auditServiceAppConfigTestDouble{})
	rec := performAppConfigRequest(http.MethodPut, "/configs/:id", "/configs/not-a-uuid", dto.UpdateAppConfig{Value: "true"}, handler.Update)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid uuid 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = performAppConfigRequest(http.MethodPut, "/configs/:id", "/configs/550e8400-e29b-41d4-a716-446655440000", map[string]any{"is_active": "yes"}, handler.Update)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid json 400, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewAppConfigHandler(&appConfigServiceTestDouble{updateErr: gorm.ErrRecordNotFound}, &auditServiceAppConfigTestDouble{})
	rec = performAppConfigRequest(http.MethodPut, "/configs/:id", "/configs/550e8400-e29b-41d4-a716-446655440000", dto.UpdateAppConfig{Value: "true"}, handler.Update)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected not found 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
