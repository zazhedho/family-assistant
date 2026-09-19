package handlerpermission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/zazhedho/family-assistant/internal/authscope"
	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	domainpermission "github.com/zazhedho/family-assistant/internal/domain/permission"
	"github.com/zazhedho/family-assistant/internal/dto"
	"github.com/zazhedho/family-assistant/pkg/filter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type permissionServiceTestDouble struct {
	permission      domainpermission.Permission
	permissions     []domainpermission.Permission
	total           int64
	createReq       dto.PermissionCreate
	updateReq       dto.PermissionUpdate
	deleteID        string
	userPermissions []domainpermission.Permission
	err             error
}

func (m *permissionServiceTestDouble) Create(ctx context.Context, req dto.PermissionCreate) (domainpermission.Permission, error) {
	m.createReq = req
	if m.err != nil {
		return domainpermission.Permission{}, m.err
	}
	return m.permission, nil
}
func (m *permissionServiceTestDouble) GetByID(ctx context.Context, id string) (domainpermission.Permission, error) {
	if m.err != nil {
		return domainpermission.Permission{}, m.err
	}
	return m.permission, nil
}
func (m *permissionServiceTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]domainpermission.Permission, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.permissions, m.total, nil
}
func (m *permissionServiceTestDouble) GetByResource(ctx context.Context, resource string) ([]domainpermission.Permission, error) {
	return m.permissions, nil
}
func (m *permissionServiceTestDouble) GetUserPermissions(ctx context.Context, userId string) ([]domainpermission.Permission, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.userPermissions, nil
}
func (m *permissionServiceTestDouble) GetRolePermissions(ctx context.Context, roleID string) ([]domainpermission.Permission, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.permissions, nil
}
func (m *permissionServiceTestDouble) Update(ctx context.Context, id string, req dto.PermissionUpdate) (domainpermission.Permission, error) {
	m.updateReq = req
	if m.err != nil {
		return domainpermission.Permission{}, m.err
	}
	return m.permission, nil
}
func (m *permissionServiceTestDouble) Delete(ctx context.Context, id string) error {
	m.deleteID = id
	return m.err
}

type auditServicePermissionTestDouble struct {
	events []domainaudit.AuditEvent
}

func (m *auditServicePermissionTestDouble) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	m.events = append(m.events, req)
	return nil
}
func (m *auditServicePermissionTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}
func (m *auditServicePermissionTestDouble) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func performPermissionRequest(
	method string,
	path string,
	body any,
	handler gin.HandlerFunc,
	scope authscope.Scope,
) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routePath, _, _ := strings.Cut(path, "?")
	router.Handle(method, routePath, func(ctx *gin.Context) {
		if scope.UserID != "" || scope.Role != "" {
			ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		}
		handler(ctx)
	})

	var reqBody *bytes.Reader
	if body == nil {
		reqBody = bytes.NewReader(nil)
	} else {
		raw, _ := json.Marshal(body)
		reqBody = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func performPermissionRawRequest(method, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routePath, _, _ := strings.Cut(path, "?")
	router.Handle(method, routePath, handler)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCreatePermissionReturnsCreatedAndWritesAudit(t *testing.T) {
	auditSvc := &auditServicePermissionTestDouble{}
	service := &permissionServiceTestDouble{permission: domainpermission.Permission{
		Id:          "perm-1",
		Name:        "list_users",
		DisplayName: "List Users",
		Resource:    "users",
		Action:      "list",
	}}
	handler := NewPermissionHandler(service, auditSvc)

	rec := performPermissionRequest(http.MethodPost, "/permissions", dto.PermissionCreate{
		Name:        "list_users",
		DisplayName: "List Users",
		Resource:    "users",
		Action:      "list",
	}, handler.Create, authscope.Scope{})

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.createReq.Name != "list_users" {
		t.Fatalf("expected service request to be captured, got %+v", service.createReq)
	}
	if len(auditSvc.events) != 1 || auditSvc.events[0].Status != domainaudit.StatusSuccess {
		t.Fatalf("expected success audit event, got %+v", auditSvc.events)
	}
}

func TestCreatePermissionRejectsInvalidJSON(t *testing.T) {
	handler := NewPermissionHandler(&permissionServiceTestDouble{}, &auditServicePermissionTestDouble{})
	rec := performPermissionRequest(http.MethodPost, "/permissions", map[string]string{"name": "ab"}, handler.Create, authscope.Scope{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetPermissionByIDReturnsNotFound(t *testing.T) {
	handler := NewPermissionHandler(
		&permissionServiceTestDouble{err: gorm.ErrRecordNotFound},
		&auditServicePermissionTestDouble{},
	)

	rec := performPermissionRequest(http.MethodGet, "/permissions/:id", nil, handler.GetByID, authscope.Scope{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetAllPermissionsReturnsPagination(t *testing.T) {
	handler := NewPermissionHandler(&permissionServiceTestDouble{
		permissions: []domainpermission.Permission{{Id: "perm-1", Name: "list_users"}},
		total:       1,
	}, &auditServicePermissionTestDouble{})

	rec := performPermissionRequest(http.MethodGet, "/permissions?page=1&limit=10", nil, handler.GetAll, authscope.Scope{})
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

func TestGetAllPermissionsReturnsServiceError(t *testing.T) {
	handler := NewPermissionHandler(&permissionServiceTestDouble{err: errors.New("database down")}, &auditServicePermissionTestDouble{})

	rec := performPermissionRequest(http.MethodGet, "/permissions", nil, handler.GetAll, authscope.Scope{})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdatePermissionMapsDuplicateError(t *testing.T) {
	handler := NewPermissionHandler(
		&permissionServiceTestDouble{err: errors.New("permission with this name already exists")},
		&auditServicePermissionTestDouble{},
	)

	rec := performPermissionRequest(http.MethodPut, "/permissions/:id", dto.PermissionUpdate{
		DisplayName: "List Users",
	}, handler.Update, authscope.Scope{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = performPermissionRawRequest(http.MethodPut, "/permissions/:id", `{`, handler.Update)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid json 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeletePermissionDelegatesToService(t *testing.T) {
	service := &permissionServiceTestDouble{permission: domainpermission.Permission{Id: "perm-1"}}
	handler := NewPermissionHandler(service, &auditServicePermissionTestDouble{})

	rec := performPermissionRequest(http.MethodDelete, "/permissions/:id", nil, handler.Delete, authscope.Scope{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.deleteID != ":id" {
		t.Fatalf("expected delete id from route param placeholder, got %q", service.deleteID)
	}

	handler = NewPermissionHandler(&permissionServiceTestDouble{err: errors.New("database down")}, &auditServicePermissionTestDouble{})
	rec = performPermissionRequest(http.MethodDelete, "/permissions/:id", nil, handler.Delete, authscope.Scope{})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected delete 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetUserPermissionsRequiresAuthScope(t *testing.T) {
	handler := NewPermissionHandler(&permissionServiceTestDouble{}, &auditServicePermissionTestDouble{})

	rec := performPermissionRequest(http.MethodGet, "/permissions/me", nil, handler.GetUserPermissions, authscope.Scope{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetUserPermissionsUsesAuthScopeUserID(t *testing.T) {
	handler := NewPermissionHandler(&permissionServiceTestDouble{
		userPermissions: []domainpermission.Permission{{Id: "perm-1", Resource: "users", Action: "list"}},
	}, &auditServicePermissionTestDouble{})

	rec := performPermissionRequest(http.MethodGet, "/permissions/me", nil, handler.GetUserPermissions, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
