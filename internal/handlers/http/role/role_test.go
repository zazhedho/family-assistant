package handlerrole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	domainaudit "starter-kit/internal/domain/audit"
	domainrole "starter-kit/internal/domain/role"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type roleServiceHandlerTestDouble struct {
	role                 domainrole.Role
	details              dto.RoleWithDetails
	roles                []domainrole.Role
	total                int64
	assigned             []string
	deletedID            string
	err                  error
	getByIDErr           error
	getAllErr            error
	deleteErr            error
	assignPermissionsErr error
	assignMenusErr       error
}

func (m *roleServiceHandlerTestDouble) Create(ctx context.Context, req dto.RoleCreate) (domainrole.Role, error) {
	return m.role, m.err
}
func (m *roleServiceHandlerTestDouble) GetByID(ctx context.Context, id string) (domainrole.Role, error) {
	if m.getByIDErr != nil {
		return domainrole.Role{}, m.getByIDErr
	}
	return m.role, nil
}
func (m *roleServiceHandlerTestDouble) GetByIDWithDetails(ctx context.Context, id string) (dto.RoleWithDetails, error) {
	return m.details, m.err
}
func (m *roleServiceHandlerTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]domainrole.Role, int64, error) {
	if m.getAllErr != nil {
		return nil, 0, m.getAllErr
	}
	return m.roles, m.total, m.err
}
func (m *roleServiceHandlerTestDouble) Update(ctx context.Context, id string, req dto.RoleUpdate) (domainrole.Role, error) {
	return m.role, m.err
}
func (m *roleServiceHandlerTestDouble) Delete(ctx context.Context, id string) error {
	m.deletedID = id
	if m.deleteErr != nil {
		return m.deleteErr
	}
	return m.err
}
func (m *roleServiceHandlerTestDouble) AssignPermissions(ctx context.Context, roleId string, req dto.AssignPermissions) error {
	m.assigned = req.PermissionIds
	if m.assignPermissionsErr != nil {
		return m.assignPermissionsErr
	}
	return m.err
}
func (m *roleServiceHandlerTestDouble) AssignMenus(ctx context.Context, roleId string, req dto.AssignMenus) error {
	m.assigned = req.MenuIds
	if m.assignMenusErr != nil {
		return m.assignMenusErr
	}
	return m.err
}
func (m *roleServiceHandlerTestDouble) GetRolePermissions(ctx context.Context, roleId string) ([]string, error) {
	return []string{"perm-old"}, nil
}
func (m *roleServiceHandlerTestDouble) GetRoleMenus(ctx context.Context, roleId string) ([]string, error) {
	return []string{"menu-old"}, nil
}

type auditServiceRoleTestDouble struct{}

func (m *auditServiceRoleTestDouble) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	return nil
}
func (m *auditServiceRoleTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}
func (m *auditServiceRoleTestDouble) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func performRoleRequest(method, routePath, requestPath string, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
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

func performRoleRawRequest(method, routePath, requestPath, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, handler)
	req := httptest.NewRequest(method, requestPath, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRoleCRUDHandlers(t *testing.T) {
	handler := NewRoleHandler(&roleServiceHandlerTestDouble{
		role:    domainrole.Role{Id: "role-1", Name: "staff"},
		details: dto.RoleWithDetails{Id: "role-1", Name: "staff"},
		roles:   []domainrole.Role{{Id: "role-1", Name: "staff"}},
		total:   1,
	}, &auditServiceRoleTestDouble{})

	if rec := performRoleRequest(http.MethodPost, "/roles", "/roles", dto.RoleCreate{Name: "staff", DisplayName: "Staff"}, handler.Create); rec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d", rec.Code)
	}
	if rec := performRoleRequest(http.MethodGet, "/roles/:id", "/roles/role-1", nil, handler.GetByID); rec.Code != http.StatusOK {
		t.Fatalf("expected get by id 200, got %d", rec.Code)
	}
	if rec := performRoleRequest(http.MethodGet, "/roles", "/roles", nil, handler.GetAll); rec.Code != http.StatusOK {
		t.Fatalf("expected get all 200, got %d", rec.Code)
	}
	if rec := performRoleRequest(http.MethodPut, "/roles/:id", "/roles/role-1", dto.RoleUpdate{DisplayName: "Staff"}, handler.Update); rec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d", rec.Code)
	}
	if rec := performRoleRequest(http.MethodDelete, "/roles/:id", "/roles/role-1", nil, handler.Delete); rec.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d", rec.Code)
	}
}

func TestRoleMutationErrorsAndAssignments(t *testing.T) {
	handler := NewRoleHandler(&roleServiceHandlerTestDouble{err: errors.New("role with this name already exists")}, &auditServiceRoleTestDouble{})
	rec := performRoleRequest(http.MethodPost, "/roles", "/roles", dto.RoleCreate{Name: "staff", DisplayName: "Staff"}, handler.Create)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate create 400, got %d", rec.Code)
	}

	service := &roleServiceHandlerTestDouble{}
	handler = NewRoleHandler(service, &auditServiceRoleTestDouble{})
	rec = performRoleRequest(http.MethodPost, "/roles/:id/permissions", "/roles/role-1/permissions", dto.AssignPermissions{PermissionIds: []string{"perm-1"}}, handler.AssignPermissions)
	if rec.Code != http.StatusOK || len(service.assigned) != 1 || service.assigned[0] != "perm-1" {
		t.Fatalf("expected permission assignment, code=%d assigned=%v", rec.Code, service.assigned)
	}

	rec = performRoleRequest(http.MethodPost, "/roles/:id/menus", "/roles/role-1/menus", dto.AssignMenus{MenuIds: []string{"menu-1"}}, handler.AssignMenus)
	if rec.Code != http.StatusOK || len(service.assigned) != 1 || service.assigned[0] != "menu-1" {
		t.Fatalf("expected menu assignment, code=%d assigned=%v", rec.Code, service.assigned)
	}
}

func TestRoleHandlersRejectInvalidJSON(t *testing.T) {
	handler := NewRoleHandler(&roleServiceHandlerTestDouble{}, &auditServiceRoleTestDouble{})

	tests := []struct {
		name      string
		method    string
		routePath string
		path      string
		call      gin.HandlerFunc
	}{
		{name: "create", method: http.MethodPost, routePath: "/roles", path: "/roles", call: handler.Create},
		{name: "update", method: http.MethodPut, routePath: "/roles/:id", path: "/roles/role-1", call: handler.Update},
		{name: "assign permissions", method: http.MethodPost, routePath: "/roles/:id/permissions", path: "/roles/role-1/permissions", call: handler.AssignPermissions},
		{name: "assign menus", method: http.MethodPost, routePath: "/roles/:id/menus", path: "/roles/role-1/menus", call: handler.AssignMenus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := performRoleRawRequest(tt.method, tt.routePath, tt.path, `{`, tt.call)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRoleHandlersServiceErrorBranches(t *testing.T) {
	handler := NewRoleHandler(&roleServiceHandlerTestDouble{err: gorm.ErrRecordNotFound}, &auditServiceRoleTestDouble{})
	rec := performRoleRequest(http.MethodGet, "/roles/:id", "/roles/role-1", nil, handler.GetByID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected get by id 404, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewRoleHandler(&roleServiceHandlerTestDouble{}, &auditServiceRoleTestDouble{})
	rec = performRoleRequest(http.MethodGet, "/roles", "/roles?page=bad", nil, handler.GetAll)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected get all bad query 400, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewRoleHandler(&roleServiceHandlerTestDouble{getAllErr: errors.New("database down")}, &auditServiceRoleTestDouble{})
	rec = performRoleRequest(http.MethodGet, "/roles", "/roles", nil, handler.GetAll)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected get all 500, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewRoleHandler(&roleServiceHandlerTestDouble{err: errors.New("cannot update system role")}, &auditServiceRoleTestDouble{})
	rec = performRoleRequest(http.MethodPut, "/roles/:id", "/roles/role-1", dto.RoleUpdate{DisplayName: "System"}, handler.Update)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected update forbidden, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewRoleHandler(&roleServiceHandlerTestDouble{deleteErr: gorm.ErrRecordNotFound}, &auditServiceRoleTestDouble{})
	rec = performRoleRequest(http.MethodDelete, "/roles/:id", "/roles/role-1", nil, handler.Delete)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected delete 404, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewRoleHandler(&roleServiceHandlerTestDouble{assignPermissionsErr: errors.New("invalid permission ID: perm-x")}, &auditServiceRoleTestDouble{})
	rec = performRoleRequest(http.MethodPost, "/roles/:id/permissions", "/roles/role-1/permissions", dto.AssignPermissions{PermissionIds: []string{"perm-x"}}, handler.AssignPermissions)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected assign permissions 400, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewRoleHandler(&roleServiceHandlerTestDouble{assignMenusErr: errors.New("database down")}, &auditServiceRoleTestDouble{})
	rec = performRoleRequest(http.MethodPost, "/roles/:id/menus", "/roles/role-1/menus", dto.AssignMenus{MenuIds: []string{"menu-1"}}, handler.AssignMenus)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected assign menus 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
