package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"starter-kit/internal/authscope"
	"starter-kit/utils"
)

func TestCORSHandlesOptionsAndSetsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS())
	router.GET("/ping", func(ctx *gin.Context) { ctx.String(http.StatusOK, "ok") })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for options, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS header, got %v", rec.Header())
	}
}

func TestSetContextIdUsesExistingRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SetContextId())
	router.GET("/ping", func(ctx *gin.Context) {
		if _, ok := ctx.Get(utils.CtxKeyId); !ok {
			t.Fatal("expected context id to be set")
		}
		ctx.String(http.StatusOK, "ok")
	})

	requestID := uuid.NewString()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-ID", requestID)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("X-Request-ID") != requestID {
		t.Fatalf("unexpected response: code=%d headers=%v", rec.Code, rec.Header())
	}
}

func TestRequestLoggerAndRecoveryMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestLogger())
	router.Use(gin.CustomRecovery(ErrorHandler))
	router.GET("/panic", func(ctx *gin.Context) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req = req.WithContext(authscope.WithContext(req.Context(), authscope.New("user-1", "Jane", "viewer", nil)))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRoleMiddlewareAllowsAndRejectsRoles(t *testing.T) {
	mdw := NewMiddleware(&authRepoTestDouble{}, &permissionRepoTestDouble{})

	rec := performMiddlewareRequest(
		testToken(t, "access", utils.RoleAdmin),
		mdw.AuthMiddleware(),
		mdw.RoleMiddleware(utils.RoleAdmin),
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin role to pass, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = performMiddlewareRequest(
		testToken(t, "access", utils.RoleViewer),
		mdw.AuthMiddleware(),
		mdw.RoleMiddleware(utils.RoleAdmin),
	)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected viewer role to be rejected, got %d: %s", rec.Code, rec.Body.String())
	}
}
