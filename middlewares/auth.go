package middlewares

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"starter-kit/infrastructure/database"
	"starter-kit/internal/authscope"
	permissioncache "starter-kit/internal/cache/permission"
	domainpermission "starter-kit/internal/domain/permission"
	interfaceauth "starter-kit/internal/interfaces/auth"
	interfacepermission "starter-kit/internal/interfaces/permission"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Middleware struct {
	BlacklistRepo   interfaceauth.RepoAuthInterface
	PermissionRepo  interfacepermission.RepoPermissionInterface
	PermissionCache *redis.Client
}

func NewMiddleware(blacklistRepo interfaceauth.RepoAuthInterface, permissionRepo interfacepermission.RepoPermissionInterface) *Middleware {
	return &Middleware{
		BlacklistRepo:   blacklistRepo,
		PermissionRepo:  permissionRepo,
		PermissionCache: database.GetRedisClient(),
	}
}

func (m *Middleware) AuthMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var (
			err       error
			logId     uuid.UUID
			logPrefix string
		)

		logId = utils.GenerateLogId(ctx)
		logPrefix = "[AuthMiddleware]"

		tokenString, dataJWT, err := utils.JwtClaims(ctx)
		if err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Invalid token; Error: %s;", logPrefix, err.Error()))
			res := response.Unauthorized(logId, "Invalid or expired token. Please login again.")
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, res)
			return
		}
		logPrefix += fmt.Sprintf("[%s][%s]", utils.InterfaceString(dataJWT["jti"]), utils.InterfaceString(dataJWT["user_id"]))

		tokenType := strings.TrimSpace(utils.InterfaceString(dataJWT["token_type"]))
		if tokenType != utils.TokenTypeAccess {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Invalid token type used on protected route", logPrefix))
			res := response.Unauthorized(logId, "Invalid token type. Please use an access token.")
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, res)
			return
		}

		isBlacklisted, err := m.BlacklistRepo.ExistsByToken(ctx.Request.Context(), tokenString)
		if err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; blacklistRepo.ExistsByToken; Error: %+v", logPrefix, err))
			res := response.InternalServerError(logId)
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, res)
			return
		}

		if isBlacklisted {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Invalid token; Error: token is blacklisted;", logPrefix))
			res := response.Unauthorized(logId, "Your session is no longer valid. Please login again.")
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, res)
			return
		}

		ctx.Set(utils.CtxKeyAuthData, dataJWT)
		ctx.Set("token", tokenString)
		ctx.Set("userId", utils.InterfaceString(dataJWT["user_id"]))
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), authscope.NewFromClaims(dataJWT, nil)))

		ctx.Next()
	}
}

func (m *Middleware) RoleMiddleware(allowedRoles ...string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var (
			logId     uuid.UUID
			logPrefix string
		)

		logId = utils.GenerateLogId(ctx)
		logPrefix = "[RoleMiddleware]"

		scope := authscope.FromContext(ctx.Request.Context())
		if strings.TrimSpace(scope.Role) == "" {
			authData, exists := ctx.Get(utils.CtxKeyAuthData)
			if !exists {
				logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; AuthData not found", logPrefix))
				res := response.Forbidden(logId, messages.AccessDenied)
				ctx.AbortWithStatusJSON(http.StatusForbidden, res)
				return
			}

			dataJWT, ok := authData.(map[string]any)
			if !ok {
				logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Invalid AuthData type", logPrefix))
				res := response.Forbidden(logId, messages.AccessDenied)
				ctx.AbortWithStatusJSON(http.StatusForbidden, res)
				return
			}

			scope = authscope.NewFromClaims(dataJWT, nil)
			ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		}

		userRole := strings.TrimSpace(scope.Role)
		if userRole == "" {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; AuthData not found", logPrefix))
			res := response.Forbidden(logId, messages.AccessDenied)
			ctx.AbortWithStatusJSON(http.StatusForbidden, res)
			return
		}

		if userRole == utils.RoleSuperAdmin {
			ctx.Next()
			return
		}

		isAllowed := slices.Contains(allowedRoles, userRole)
		if !isAllowed {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; User with role '%s' tried to access a restricted route;", logPrefix, userRole))
			res := response.Forbidden(logId, messages.AccessDenied)
			ctx.AbortWithStatusJSON(http.StatusForbidden, res)
			return
		}

		ctx.Next()
	}
}

func (m *Middleware) PermissionMiddleware(resource, action string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var (
			logId     uuid.UUID
			logPrefix string
		)

		logId = utils.GenerateLogId(ctx)
		logPrefix = "[PermissionMiddleware]"

		authData, exists := ctx.Get(utils.CtxKeyAuthData)
		if !exists {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; AuthData not found", logPrefix))
			res := response.Forbidden(logId, messages.AccessDenied)
			ctx.AbortWithStatusJSON(http.StatusForbidden, res)
			return
		}
		dataJWT, ok := authData.(map[string]any)
		if !ok {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Invalid AuthData type", logPrefix))
			res := response.Forbidden(logId, messages.AccessDenied)
			ctx.AbortWithStatusJSON(http.StatusForbidden, res)
			return
		}

		userRole := strings.TrimSpace(utils.InterfaceString(dataJWT["role"]))
		if userRole == "" {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; there is no role user", logPrefix))
			res := response.Forbidden(logId, messages.AccessDenied)
			ctx.AbortWithStatusJSON(http.StatusForbidden, res)
			return
		}

		// Superadmin bypasses all permission checks
		if userRole == utils.RoleSuperAdmin {
			ctx.Next()
			return
		}

		userId := strings.TrimSpace(utils.InterfaceString(dataJWT["user_id"]))
		if userId == "" {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Invalid token claims: user_id is empty", logPrefix))
			res := response.Unauthorized(logId, "Invalid or expired token. Please login again.")
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, res)
			return
		}

		targetResource := strings.TrimSpace(resource)
		targetAction := strings.TrimSpace(action)

		permissionKeys, cacheHit := permissioncache.GetUserPermissionKeys(ctx.Request.Context(), m.PermissionCache, userId)
		if !cacheHit {
			permissions, err := m.PermissionRepo.GetUserPermissions(ctx.Request.Context(), userId)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					logger.WriteLogWithContext(ctx, logger.LogLevelWarn, fmt.Sprintf("%s; User '%s' not found when loading permissions", logPrefix, userId))
					res := response.Forbidden(logId, messages.AccessDenied)
					ctx.AbortWithStatusJSON(http.StatusForbidden, res)
					return
				}

				logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Failed to get user permissions: %s", logPrefix, err.Error()))
				res := response.InternalServerError(logId)
				ctx.AbortWithStatusJSON(http.StatusInternalServerError, res)
				return
			}

			permissionKeys = permissionKeysFromPermissions(permissions)
			permissioncache.SetUserPermissionKeys(ctx.Request.Context(), m.PermissionCache, userId, permissionKeys)
		}

		dataJWT["permissions"] = permissionKeys
		ctx.Set(utils.CtxKeyAuthData, dataJWT)
		ctx.Set("permissions", permissionKeys)

		scope := authscope.NewFromClaims(dataJWT, permissionKeys)
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))

		if !scope.Has(targetResource, targetAction) {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; User '%s' lacks permission '%s:%s'", logPrefix, userId, targetResource, targetAction))
			res := response.Forbidden(logId, messages.AccessDenied)
			ctx.AbortWithStatusJSON(http.StatusForbidden, res)
			return
		}

		ctx.Next()
	}
}

func permissionKeysFromPermissions(permissions []domainpermission.Permission) []string {
	permissionKeys := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		permissionKey := authscope.PermissionKey(perm.Resource, perm.Action)
		if permissionKey != "" {
			permissionKeys = append(permissionKeys, permissionKey)
		}
	}
	return permissionKeys
}
