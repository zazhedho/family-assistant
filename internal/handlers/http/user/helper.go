package handleruser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultConfigPublicRegistrationEnabled = "auth.public_registration_enabled"
	defaultConfigRegisterOTPEnabled        = "auth.register_otp_enabled"
	defaultConfigPasswordResetEmailEnabled = "auth.password_reset_email_enabled"
)

func (h *HandlerUser) respondTooManyLoginAttempts(ctx *gin.Context, logId uuid.UUID, ttl time.Duration) {
	if ttl > 0 {
		ctx.Header("Retry-After", strconv.Itoa(int(ttl.Seconds())))
	}

	message := "Too many login attempts. Please try again later."
	if ttl > 0 {
		message = fmt.Sprintf("Too many login attempts. Try again in %d seconds.", int(ttl.Seconds()))
	}

	res := response.Response(http.StatusTooManyRequests, messages.MsgSomethingWrong, logId, nil)
	res.Error = response.Errors{Code: http.StatusTooManyRequests, Message: message}
	ctx.AbortWithStatusJSON(http.StatusTooManyRequests, res)
}

func (h *HandlerUser) respondThrottle(ctx *gin.Context, logId uuid.UUID, ttl time.Duration, message string) {
	if ttl > 0 {
		ctx.Header("Retry-After", strconv.Itoa(int(ttl.Seconds())))
	}

	if message == "" {
		message = "Too many requests. Please try again later."
	}

	res := response.Response(http.StatusTooManyRequests, messages.MsgSomethingWrong, logId, nil)
	res.Error = response.Errors{Code: http.StatusTooManyRequests, Message: message}
	ctx.AbortWithStatusJSON(http.StatusTooManyRequests, res)
}

func (h *HandlerUser) isRuntimeConfigEnabled(ctx context.Context, configKey string, fallback bool) (bool, error) {
	if h.AppConfigService == nil {
		return fallback, nil
	}
	return h.AppConfigService.IsEnabled(ctx, configKey, fallback)
}

func buildAuthTokenResponse(accessToken string, refreshToken string) map[string]any {
	data := map[string]any{
		"access_token":     accessToken,
		"token_type":       "Bearer",
		"expires_in_hours": utils.GetEnv("JWT_EXP", 24),
	}

	if refreshToken != "" {
		data["refresh_token"] = refreshToken
		data["refresh_expires_in_hours"] = utils.GetEnv("REFRESH_TOKEN_EXP_HOURS", 168)
	}

	return data
}

func userMutationErrorResponse(logId uuid.UUID, err error) (int, *response.ApiResponse) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound, response.ErrorResponse(http.StatusNotFound, messages.MsgNotFound, logId, "user not found")
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgExists, logId, "email or phone already exists")
	}

	errMsg := err.Error()
	switch {
	case errMsg == "user not found":
		return http.StatusNotFound, response.ErrorResponse(http.StatusNotFound, messages.MsgNotFound, logId, "user not found")
	case errMsg == "invalid or expired token":
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgSomethingWrong, logId, "invalid or expired reset token")
	case strings.HasPrefix(errMsg, "access denied:"),
		strings.Contains(errMsg, "superadmin"):
		return http.StatusForbidden, response.Forbidden(logId, messages.AccessDenied)
	case strings.Contains(errMsg, "already exists"):
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgExists, logId, errMsg)
	case strings.HasPrefix(errMsg, "invalid role:"),
		strings.HasPrefix(errMsg, "password must "),
		strings.HasPrefix(errMsg, "new password must "):
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgSomethingWrong, logId, errMsg)
	default:
		return http.StatusInternalServerError, response.InternalServerError(logId)
	}
}

func impersonationErrorResponse(logId uuid.UUID, err error) (int, *response.ApiResponse) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound, response.ErrorResponse(http.StatusNotFound, messages.MsgNotFound, logId, "user not found")
	}

	errMsg := err.Error()
	switch {
	case strings.HasPrefix(errMsg, "cannot impersonate"):
		return http.StatusForbidden, response.Forbidden(logId, messages.AccessDenied)
	case strings.HasPrefix(errMsg, "cannot start"),
		strings.HasPrefix(errMsg, "target user id"),
		strings.HasPrefix(errMsg, "original user id"),
		strings.HasPrefix(errMsg, "current session"):
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgSomethingWrong, logId, errMsg)
	default:
		return http.StatusInternalServerError, response.InternalServerError(logId)
	}
}

func buildImpersonationClaimsOverrideFromClaims(claims map[string]any) *utils.AppClaims {
	if claims == nil || !utils.InterfaceBool(claims["is_impersonated"]) {
		return nil
	}

	return &utils.AppClaims{
		IsImpersonated:   true,
		OriginalUserId:   utils.InterfaceString(claims["original_user_id"]),
		OriginalUsername: utils.InterfaceString(claims["original_username"]),
		OriginalRole:     utils.InterfaceString(claims["original_role"]),
	}
}
