package handlermenu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"starter-kit/internal/authscope"
	domainaudit "starter-kit/internal/domain/audit"
	domainmenu "starter-kit/internal/domain/menu"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"testing"

	"github.com/gin-gonic/gin"
)

type menuServiceHandlerTestDouble struct {
	menu      domainmenu.MenuItem
	menus     []domainmenu.MenuItem
	total     int64
	updateReq dto.MenuUpdate
	err       error
}

func (m *menuServiceHandlerTestDouble) GetByID(ctx context.Context, id string) (domainmenu.MenuItem, error) {
	if m.err != nil {
		return domainmenu.MenuItem{}, m.err
	}
	return m.menu, nil
}
func (m *menuServiceHandlerTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]domainmenu.MenuItem, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.menus, m.total, nil
}
func (m *menuServiceHandlerTestDouble) GetActiveMenus(ctx context.Context) ([]domainmenu.MenuItem, error) {
	return m.menus, m.err
}
func (m *menuServiceHandlerTestDouble) GetUserMenus(ctx context.Context, userId string) ([]domainmenu.MenuItem, error) {
	return m.menus, m.err
}
func (m *menuServiceHandlerTestDouble) Update(ctx context.Context, id string, req dto.MenuUpdate) (domainmenu.MenuItem, error) {
	m.updateReq = req
	if m.err != nil {
		return domainmenu.MenuItem{}, m.err
	}
	return m.menu, nil
}

type auditServiceMenuTestDouble struct{}

func (m *auditServiceMenuTestDouble) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	return nil
}
func (m *auditServiceMenuTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}
func (m *auditServiceMenuTestDouble) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func performMenuRequest(method, routePath, requestPath string, body any, handler gin.HandlerFunc, scope authscope.Scope) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, func(ctx *gin.Context) {
		if scope.UserID != "" {
			ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
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

func TestMenuReadHandlers(t *testing.T) {
	handler := NewMenuHandler(&menuServiceHandlerTestDouble{
		menu:  domainmenu.MenuItem{Id: "menu-1", Name: "users"},
		menus: []domainmenu.MenuItem{{Id: "menu-1", Name: "users"}},
		total: 1,
	}, &auditServiceMenuTestDouble{})

	if rec := performMenuRequest(http.MethodGet, "/menus/:id", "/menus/menu-1", nil, handler.GetByID, authscope.Scope{}); rec.Code != http.StatusOK {
		t.Fatalf("expected get by id 200, got %d", rec.Code)
	}
	if rec := performMenuRequest(http.MethodGet, "/menus", "/menus?page=1&limit=10", nil, handler.GetAll, authscope.Scope{}); rec.Code != http.StatusOK {
		t.Fatalf("expected get all 200, got %d", rec.Code)
	}
	if rec := performMenuRequest(http.MethodGet, "/menus/active", "/menus/active", nil, handler.GetActiveMenus, authscope.Scope{}); rec.Code != http.StatusOK {
		t.Fatalf("expected active menus 200, got %d", rec.Code)
	}
}

func TestMenuReadHandlersMapServiceErrors(t *testing.T) {
	handler := NewMenuHandler(&menuServiceHandlerTestDouble{err: errors.New("database down")}, &auditServiceMenuTestDouble{})

	tests := []struct {
		name      string
		method    string
		routePath string
		path      string
		call      gin.HandlerFunc
		scope     authscope.Scope
		want      int
	}{
		{name: "get by id", method: http.MethodGet, routePath: "/menus/:id", path: "/menus/menu-1", call: handler.GetByID, want: http.StatusNotFound},
		{name: "get all", method: http.MethodGet, routePath: "/menus", path: "/menus", call: handler.GetAll, want: http.StatusInternalServerError},
		{name: "get active", method: http.MethodGet, routePath: "/menus/active", path: "/menus/active", call: handler.GetActiveMenus, want: http.StatusInternalServerError},
		{name: "get user menus", method: http.MethodGet, routePath: "/menus/me", path: "/menus/me", call: handler.GetUserMenus, scope: authscope.New("user-1", "Jane", "viewer", nil), want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := performMenuRequest(tt.method, tt.routePath, tt.path, nil, tt.call, tt.scope)
			if rec.Code != tt.want {
				t.Fatalf("expected %d, got %d: %s", tt.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGetUserMenusRequiresScope(t *testing.T) {
	handler := NewMenuHandler(&menuServiceHandlerTestDouble{}, &auditServiceMenuTestDouble{})
	rec := performMenuRequest(http.MethodGet, "/menus/me", "/menus/me", nil, handler.GetUserMenus, authscope.Scope{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	handler = NewMenuHandler(&menuServiceHandlerTestDouble{menus: []domainmenu.MenuItem{{Id: "menu-1", Name: "users"}}}, &auditServiceMenuTestDouble{})
	rec = performMenuRequest(http.MethodGet, "/menus/me", "/menus/me", nil, handler.GetUserMenus, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestUpdateMenuMapsSuccessAndError(t *testing.T) {
	service := &menuServiceHandlerTestDouble{menu: domainmenu.MenuItem{Id: "menu-1", Name: "users"}}
	handler := NewMenuHandler(service, &auditServiceMenuTestDouble{})
	rec := performMenuRequest(http.MethodPut, "/menus/:id", "/menus/menu-1", dto.MenuUpdate{DisplayName: "Users"}, handler.Update, authscope.Scope{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.updateReq.DisplayName != "Users" {
		t.Fatalf("expected update request, got %+v", service.updateReq)
	}

	handler = NewMenuHandler(&menuServiceHandlerTestDouble{err: errors.New("database down")}, &auditServiceMenuTestDouble{})
	rec = performMenuRequest(http.MethodPut, "/menus/:id", "/menus/menu-1", dto.MenuUpdate{DisplayName: "Users"}, handler.Update, authscope.Scope{})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	rec = performMenuRequest(http.MethodPut, "/menus/:id", "/menus/menu-1", map[string]any{"is_active": "bad"}, handler.Update, authscope.Scope{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid update 400, got %d", rec.Code)
	}
}
