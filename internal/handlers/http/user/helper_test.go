package handleruser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	domainappconfig "starter-kit/internal/domain/appconfig"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type appConfigServiceUserTestDouble struct {
	enabled bool
	err     error
}

func (m *appConfigServiceUserTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]domainappconfig.AppConfig, int64, error) {
	return nil, 0, nil
}
func (m *appConfigServiceUserTestDouble) GetByID(ctx context.Context, id string) (domainappconfig.AppConfig, error) {
	return domainappconfig.AppConfig{}, nil
}
func (m *appConfigServiceUserTestDouble) GetByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, error) {
	return domainappconfig.AppConfig{}, nil
}
func (m *appConfigServiceUserTestDouble) Update(ctx context.Context, id string, req dto.UpdateAppConfig) (domainappconfig.AppConfig, error) {
	return domainappconfig.AppConfig{}, nil
}
func (m *appConfigServiceUserTestDouble) GetString(ctx context.Context, configKey string, fallback string) (string, error) {
	return fallback, nil
}
func (m *appConfigServiceUserTestDouble) GetBool(ctx context.Context, configKey string, fallback bool) (bool, error) {
	return fallback, nil
}
func (m *appConfigServiceUserTestDouble) GetInt(ctx context.Context, configKey string, fallback int) (int, error) {
	return fallback, nil
}
func (m *appConfigServiceUserTestDouble) GetDuration(ctx context.Context, configKey string, fallback time.Duration) (time.Duration, error) {
	return fallback, nil
}
func (m *appConfigServiceUserTestDouble) DecodeJSON(ctx context.Context, configKey string, target any) error {
	return nil
}
func (m *appConfigServiceUserTestDouble) IsEnabled(ctx context.Context, configKey string, fallback bool) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.enabled, nil
}

func TestBuildAuthTokenResponse(t *testing.T) {
	t.Setenv("JWT_EXP", "12")
	t.Setenv("REFRESH_TOKEN_EXP_HOURS", "72")

	got := buildAuthTokenResponse("access", "refresh")
	if got["access_token"] != "access" || got["refresh_token"] != "refresh" {
		t.Fatalf("unexpected token response: %+v", got)
	}
	if got["expires_in_hours"] != 12 || got["refresh_expires_in_hours"] != 72 {
		t.Fatalf("unexpected expiry metadata: %+v", got)
	}
}

func TestUserMutationErrorResponseMapping(t *testing.T) {
	logID := uuid.New()
	tests := []struct {
		err  error
		code int
	}{
		{gorm.ErrRecordNotFound, http.StatusNotFound},
		{gorm.ErrDuplicatedKey, http.StatusBadRequest},
		{errors.New("email already exists"), http.StatusBadRequest},
		{errors.New("access denied: missing permission"), http.StatusForbidden},
		{errors.New("invalid role: admin"), http.StatusBadRequest},
		{errors.New("database down"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		code, _ := userMutationErrorResponse(logID, tt.err)
		if code != tt.code {
			t.Fatalf("error %v: expected %d, got %d", tt.err, tt.code, code)
		}
	}
}

func TestImpersonationErrorResponseMapping(t *testing.T) {
	logID := uuid.New()
	tests := []struct {
		err  error
		code int
	}{
		{gorm.ErrRecordNotFound, http.StatusNotFound},
		{errors.New("cannot impersonate superadmin users"), http.StatusForbidden},
		{errors.New("target user id is required"), http.StatusBadRequest},
		{errors.New("database down"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		code, _ := impersonationErrorResponse(logID, tt.err)
		if code != tt.code {
			t.Fatalf("error %v: expected %d, got %d", tt.err, tt.code, code)
		}
	}
}

func TestBuildImpersonationClaimsOverrideFromClaims(t *testing.T) {
	if got := buildImpersonationClaimsOverrideFromClaims(nil); got != nil {
		t.Fatalf("expected nil claims override, got %+v", got)
	}

	got := buildImpersonationClaimsOverrideFromClaims(map[string]any{
		"is_impersonated":   true,
		"original_user_id":  "admin-1",
		"original_username": "Admin",
		"original_role":     "admin",
	})
	if got == nil || !got.IsImpersonated || got.OriginalUserId != "admin-1" {
		t.Fatalf("unexpected claims override: %+v", got)
	}
}

func TestRuntimeConfigAndThrottleResponses(t *testing.T) {
	handler := &HandlerUser{}
	enabled, err := handler.isRuntimeConfigEnabled(context.Background(), "missing", true)
	if err != nil || !enabled {
		t.Fatalf("expected fallback config value, enabled=%v err=%v", enabled, err)
	}

	handler.AppConfigService = &appConfigServiceUserTestDouble{enabled: false}
	enabled, err = handler.isRuntimeConfigEnabled(context.Background(), "auth.enabled", true)
	if err != nil || enabled {
		t.Fatalf("expected service config value false, enabled=%v err=%v", enabled, err)
	}

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	handler.respondTooManyLoginAttempts(ctx, uuid.New(), 5*time.Second)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "5" {
		t.Fatalf("unexpected too many attempts response: code=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(rec)
	handler.respondThrottle(ctx, uuid.New(), 3*time.Second, "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "3" {
		t.Fatalf("unexpected throttle response: code=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
}
