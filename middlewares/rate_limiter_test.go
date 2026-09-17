package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	redismock "github.com/go-redis/redismock/v9"
)

func TestIPRateLimitMiddlewareBypassesWhenRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/register", IPRateLimitMiddleware(nil, "register", 1, time.Minute), func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/register", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEndpointRateLimitMiddlewareBypassesWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/items", EndpointRateLimitMiddleware(nil, 0, time.Minute), func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/items", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIPRateLimitMiddlewareBlocksWhenLimitExceeded(t *testing.T) {
	client, mock := redismock.NewClientMock()
	key := "rate_limit:register:1.2.3.4"
	mock.ExpectIncr(key).SetVal(2)
	mock.ExpectTTL(key).SetVal(30 * time.Second)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/register", IPRateLimitMiddleware(client, "register", 1, time.Minute), func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/register", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("expected 429 with retry header, got %d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestEndpointRateLimitMiddlewareAllowsFirstRequest(t *testing.T) {
	client, mock := redismock.NewClientMock()
	key := "rate_limit:endpoint:/items:1.2.3.4"
	mock.ExpectIncr(key).SetVal(1)
	mock.ExpectExpire(key, time.Minute).SetVal(true)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/items", EndpointRateLimitMiddleware(client, 1, time.Minute), func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/items", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
