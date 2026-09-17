package handleraudit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"testing"

	"github.com/gin-gonic/gin"
)

type auditServiceTestDouble struct {
	rows  []dto.AuditTrailResponse
	row   dto.AuditTrailResponse
	total int64
	err   error
}

func (m *auditServiceTestDouble) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	return nil
}
func (m *auditServiceTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.rows, m.total, nil
}
func (m *auditServiceTestDouble) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	if m.err != nil {
		return dto.AuditTrailResponse{}, m.err
	}
	return m.row, nil
}

func performAuditRequest(method, routePath, requestPath string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, handler)
	req := httptest.NewRequest(method, requestPath, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAuditGetAllReturnsPagination(t *testing.T) {
	handler := NewAuditHandler(&auditServiceTestDouble{
		rows:  []dto.AuditTrailResponse{{ID: "audit-1", Resource: "users"}},
		total: 1,
	})

	rec := performAuditRequest(http.MethodGet, "/audits", "/audits?page=1&limit=20", handler.GetAll)
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

func TestAuditGetAllMapsServiceError(t *testing.T) {
	handler := NewAuditHandler(&auditServiceTestDouble{err: errors.New("database down")})

	rec := performAuditRequest(http.MethodGet, "/audits", "/audits", handler.GetAll)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuditGetByIDValidatesUUIDAndReturnsNotFound(t *testing.T) {
	handler := NewAuditHandler(&auditServiceTestDouble{})

	rec := performAuditRequest(http.MethodGet, "/audits/:id", "/audits/not-a-uuid", handler.GetByID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewAuditHandler(&auditServiceTestDouble{err: errors.New("not found")})
	rec = performAuditRequest(http.MethodGet, "/audits/:id", "/audits/550e8400-e29b-41d4-a716-446655440000", handler.GetByID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
