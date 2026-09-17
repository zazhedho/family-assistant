package router

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/zazhedho/family-assistant/infrastructure/database"
	mediaInfrastructure "github.com/zazhedho/family-assistant/infrastructure/media"
	permissioncache "github.com/zazhedho/family-assistant/internal/cache/permission"
	appConfigHandler "github.com/zazhedho/family-assistant/internal/handlers/http/appconfig"
	auditHandler "github.com/zazhedho/family-assistant/internal/handlers/http/audit"
	locationHandler "github.com/zazhedho/family-assistant/internal/handlers/http/location"
	mediaHandler "github.com/zazhedho/family-assistant/internal/handlers/http/media"
	menuHandler "github.com/zazhedho/family-assistant/internal/handlers/http/menu"
	permissionHandler "github.com/zazhedho/family-assistant/internal/handlers/http/permission"
	roleHandler "github.com/zazhedho/family-assistant/internal/handlers/http/role"
	sessionHandler "github.com/zazhedho/family-assistant/internal/handlers/http/session"
	userHandler "github.com/zazhedho/family-assistant/internal/handlers/http/user"
	interfaceaudit "github.com/zazhedho/family-assistant/internal/interfaces/audit"
	interfacepermission "github.com/zazhedho/family-assistant/internal/interfaces/permission"
	interfacesession "github.com/zazhedho/family-assistant/internal/interfaces/session"
	appConfigRepo "github.com/zazhedho/family-assistant/internal/repositories/appconfig"
	auditRepo "github.com/zazhedho/family-assistant/internal/repositories/audit"
	authRepo "github.com/zazhedho/family-assistant/internal/repositories/auth"
	locationRepo "github.com/zazhedho/family-assistant/internal/repositories/location"
	mediaRepo "github.com/zazhedho/family-assistant/internal/repositories/media"
	menuRepo "github.com/zazhedho/family-assistant/internal/repositories/menu"
	otpRepo "github.com/zazhedho/family-assistant/internal/repositories/otp"
	permissionRepo "github.com/zazhedho/family-assistant/internal/repositories/permission"
	resetRepo "github.com/zazhedho/family-assistant/internal/repositories/reset"
	roleRepo "github.com/zazhedho/family-assistant/internal/repositories/role"
	sessionRepo "github.com/zazhedho/family-assistant/internal/repositories/session"
	userRepo "github.com/zazhedho/family-assistant/internal/repositories/user"
	appConfigSvc "github.com/zazhedho/family-assistant/internal/services/appconfig"
	auditSvc "github.com/zazhedho/family-assistant/internal/services/audit"
	locationSvc "github.com/zazhedho/family-assistant/internal/services/location"
	mediaSvc "github.com/zazhedho/family-assistant/internal/services/media"
	menuSvc "github.com/zazhedho/family-assistant/internal/services/menu"
	otpSvc "github.com/zazhedho/family-assistant/internal/services/otp"
	permissionSvc "github.com/zazhedho/family-assistant/internal/services/permission"
	resetSvc "github.com/zazhedho/family-assistant/internal/services/reset"
	roleSvc "github.com/zazhedho/family-assistant/internal/services/role"
	sessionSvc "github.com/zazhedho/family-assistant/internal/services/session"
	userSvc "github.com/zazhedho/family-assistant/internal/services/user"
	"github.com/zazhedho/family-assistant/middlewares"
	"github.com/zazhedho/family-assistant/pkg/config"
	"github.com/zazhedho/family-assistant/pkg/logger"
	"github.com/zazhedho/family-assistant/pkg/mailer"
	"github.com/zazhedho/family-assistant/pkg/security"
	"github.com/zazhedho/family-assistant/utils"
)

type Routes struct {
	App *gin.Engine
	DB  *gorm.DB
}

func NewRoutes() *Routes {
	app := gin.Default()

	app.Use(middlewares.CORS())
	app.Use(gin.CustomRecovery(middlewares.ErrorHandler))
	app.Use(middlewares.SetContextId())
	app.Use(middlewares.RequestLogger())

	app.GET("/healthcheck", func(ctx *gin.Context) {
		logger.WriteLogWithContext(ctx, logger.LogLevelDebug, "ClientIP: "+ctx.ClientIP())
		ctx.JSON(http.StatusOK, gin.H{
			"message": "OK!!",
		})
	})

	return &Routes{
		App: app,
	}
}

func (r *Routes) auditService() interfaceaudit.ServiceAuditInterface {
	return auditSvc.NewAuditService(auditRepo.NewAuditRepo(r.DB))
}

func (r *Routes) permissionRepo() interfacepermission.RepoPermissionInterface {
	return permissionRepo.NewPermissionRepo(r.DB)
}

func (r *Routes) middleware(permissionRepo interfacepermission.RepoPermissionInterface) *middlewares.Middleware {
	return middlewares.NewMiddleware(authRepo.NewBlacklistRepo(r.DB), permissionRepo)
}

func (r *Routes) UserRoutes() {
	blacklistRepo := authRepo.NewBlacklistRepo(r.DB)
	repo := userRepo.NewUserRepo(r.DB)
	rRepo := roleRepo.NewRoleRepo(r.DB)
	pRepo := r.permissionRepo()
	redisClient := database.GetRedisClient()
	permissionInvalidator := permissioncache.NewInvalidator(redisClient)
	uc := userSvc.NewUserService(repo, blacklistRepo, rRepo, pRepo, permissionInvalidator)
	var userSessionSvc interfacesession.ServiceSessionInterface
	repoAppConfig := appConfigRepo.NewAppConfigRepo(r.DB)
	svcAppConfig := appConfigSvc.NewAppConfigService(repoAppConfig)

	// Setup login limiter if Redis is available
	var loginLimiter security.LoginLimiter
	var registerOTPService = otpSvc.NewOTPService(nil, nil, config.LoadOTPConfig())
	var passwordResetService = resetSvc.NewPasswordResetService(nil, nil, config.LoadPasswordResetConfig())
	if redisClient != nil {
		loginLimiter = security.NewRedisLoginLimiter(
			redisClient,
			utils.GetEnv("LOGIN_ATTEMPT_LIMIT", 5),
			time.Duration(utils.GetEnv("LOGIN_ATTEMPT_WINDOW_SECONDS", 60))*time.Second,
			time.Duration(utils.GetEnv("LOGIN_BLOCK_DURATION_SECONDS", 300))*time.Second,
		)

		sRepo := sessionRepo.NewSessionRepository(redisClient)
		userSessionSvc = sessionSvc.NewSessionService(sRepo)

		sender, err := mailer.NewBrevoSenderFromEnv()
		if err != nil {
			logger.WriteLog(logger.LogLevelWarn, "Email sender not configured: ", err)
		} else {
			registerOTPService = otpSvc.NewOTPService(otpRepo.NewOTPRepository(redisClient), sender, config.LoadOTPConfig())
			passwordResetService = resetSvc.NewPasswordResetService(resetRepo.NewPasswordResetRepository(redisClient), sender, config.LoadPasswordResetConfig())
		}
	}

	h := userHandler.NewUserHandler(uc, blacklistRepo, userSessionSvc, loginLimiter, r.auditService(), svcAppConfig, registerOTPService, passwordResetService)
	mdw := middlewares.NewMiddleware(blacklistRepo, pRepo)

	// Setup register rate limiter
	registerLimit := utils.GetEnv("REGISTER_RATE_LIMIT", 5)
	registerWindowSeconds := utils.GetEnv("REGISTER_RATE_WINDOW_SECONDS", 60)
	if registerWindowSeconds <= 0 {
		registerWindowSeconds = 60
	}
	registerLimiter := middlewares.IPRateLimitMiddleware(
		redisClient,
		"user_register",
		registerLimit,
		time.Duration(registerWindowSeconds)*time.Second,
	)

	user := r.App.Group("/api/user")
	{
		user.GET("/register/status", h.GetRegisterStatus)
		user.POST("/register", registerLimiter, h.Register)
		user.POST("/register/otp/send", registerLimiter, h.SendRegisterOTP)
		user.POST("/login", h.Login)
		user.POST("/google/login", h.GoogleLogin)
		user.POST("/refresh-token", h.RefreshToken)
		user.POST("/forgot-password", h.ForgotPassword)
		user.POST("/reset-password", h.ResetPassword)

		userPriv := user.Group("").Use(mdw.AuthMiddleware())
		{
			userPriv.POST("/logout", h.Logout)
			userPriv.GET("", h.GetUserByAuth)
			userPriv.GET("/:id", mdw.PermissionMiddleware("users", "view"), h.GetUserById)
			userPriv.POST("/:id/impersonate", mdw.PermissionMiddleware("users", "impersonate"), h.ImpersonateUser)
			userPriv.POST("/stop-impersonation", h.StopImpersonation)
			userPriv.PUT("", h.Update)
			userPriv.PUT("/:id", mdw.PermissionMiddleware("users", "update"), h.UpdateUserById)
			userPriv.PUT("/change/password", h.ChangePassword)
			userPriv.DELETE("", h.Delete)
			userPriv.DELETE("/:id", mdw.PermissionMiddleware("users", "delete"), h.DeleteUserById)

			// Admin create user endpoint (with role selection)
			userPriv.POST("", mdw.PermissionMiddleware("users", "create"), h.AdminCreateUser)
		}
	}

	r.App.GET("/api/users", mdw.AuthMiddleware(), mdw.PermissionMiddleware("users", "list"), h.GetAllUsers)
}

func (r *Routes) RoleRoutes() {
	repoRole := roleRepo.NewRoleRepo(r.DB)
	repoPermission := r.permissionRepo()
	repoMenu := menuRepo.NewMenuRepo(r.DB)
	permissionInvalidator := permissioncache.NewInvalidator(database.GetRedisClient())
	svc := roleSvc.NewRoleService(repoRole, repoPermission, repoMenu, permissionInvalidator)
	h := roleHandler.NewRoleHandler(svc, r.auditService())
	mdw := r.middleware(repoPermission)

	// List endpoints
	r.App.GET("/api/roles", mdw.AuthMiddleware(), mdw.PermissionMiddleware("roles", "list"), h.GetAll)

	// CRUD endpoints
	role := r.App.Group("/api/role").Use(mdw.AuthMiddleware())
	{
		role.POST("", mdw.PermissionMiddleware("roles", "create"), h.Create)
		role.GET("/:id", mdw.PermissionMiddleware("roles", "view"), h.GetByID)
		role.PUT("/:id", mdw.PermissionMiddleware("roles", "update"), h.Update)
		role.DELETE("/:id", mdw.PermissionMiddleware("roles", "delete"), h.Delete)

		// Permission and menu assignment
		role.POST("/:id/permissions", mdw.PermissionMiddleware("roles", "assign_permissions"), h.AssignPermissions)
	}
}

func (r *Routes) PermissionRoutes() {
	repo := r.permissionRepo()
	permissionInvalidator := permissioncache.NewInvalidator(database.GetRedisClient())
	svc := permissionSvc.NewPermissionService(repo, permissionInvalidator)
	h := permissionHandler.NewPermissionHandler(svc, r.auditService())
	mdw := r.middleware(repo)

	// List endpoints
	r.App.GET("/api/permissions", mdw.AuthMiddleware(), mdw.PermissionMiddleware("permissions", "list"), h.GetAll)

	// Get current user's permissions
	r.App.GET("/api/permissions/me", mdw.AuthMiddleware(), h.GetUserPermissions)

	// CRUD endpoints
	permission := r.App.Group("/api/permission").Use(mdw.AuthMiddleware())
	{
		permission.POST("", mdw.PermissionMiddleware("permissions", "create"), h.Create)
		permission.GET("/:id", mdw.PermissionMiddleware("permissions", "view"), h.GetByID)
		permission.PUT("/:id", mdw.PermissionMiddleware("permissions", "update"), h.Update)
		permission.DELETE("/:id", mdw.PermissionMiddleware("permissions", "delete"), h.Delete)
	}
}

func (r *Routes) MenuRoutes() {
	repo := menuRepo.NewMenuRepo(r.DB)
	pRepo := r.permissionRepo()
	svc := menuSvc.NewMenuService(repo, pRepo)
	h := menuHandler.NewMenuHandler(svc, r.auditService())
	mdw := r.middleware(pRepo)

	// Public endpoints for authenticated users
	r.App.GET("/api/menus/active", mdw.AuthMiddleware(), h.GetActiveMenus)
	r.App.GET("/api/menus/me", mdw.AuthMiddleware(), h.GetUserMenus)

	// List endpoints
	r.App.GET("/api/menus", mdw.AuthMiddleware(), mdw.PermissionMiddleware("menus", "list"), h.GetAll)

	// CRUD endpoints
	menu := r.App.Group("/api/menu").Use(mdw.AuthMiddleware())
	{
		menu.GET("/:id", mdw.PermissionMiddleware("menus", "view"), h.GetByID)
		menu.PUT("/:id", mdw.PermissionMiddleware("menus", "update"), h.Update)
	}
}

func (r *Routes) AppConfigRoutes() {
	repo := appConfigRepo.NewAppConfigRepo(r.DB)
	svc := appConfigSvc.NewAppConfigService(repo)
	h := appConfigHandler.NewAppConfigHandler(svc, r.auditService())
	pRepo := r.permissionRepo()
	mdw := r.middleware(pRepo)

	r.App.GET("/api/configs", mdw.AuthMiddleware(), mdw.PermissionMiddleware("configs", "list"), h.GetAll)

	appConfig := r.App.Group("/api/config").Use(mdw.AuthMiddleware())
	{
		appConfig.GET("/:id", mdw.PermissionMiddleware("configs", "view"), h.GetByID)
		appConfig.PUT("/:id", mdw.PermissionMiddleware("configs", "update"), h.Update)
	}
}

func (r *Routes) AuditRoutes() {
	repo := auditRepo.NewAuditRepo(r.DB)
	svc := auditSvc.NewAuditService(repo)
	h := auditHandler.NewAuditHandler(svc)
	pRepo := r.permissionRepo()
	mdw := r.middleware(pRepo)

	r.App.GET("/api/audits", mdw.AuthMiddleware(), mdw.PermissionMiddleware("audits", "list"), h.GetAll)

	audit := r.App.Group("/api/audit").Use(mdw.AuthMiddleware())
	{
		audit.GET("/:id", mdw.PermissionMiddleware("audits", "view"), h.GetByID)
	}
}

func (r *Routes) SessionRoutes() {
	redisClient := database.GetRedisClient()
	if redisClient == nil {
		logger.WriteLog(logger.LogLevelDebug, "Redis not available, session routes will not be registered")
		return
	}

	repo := sessionRepo.NewSessionRepository(redisClient)
	svc := sessionSvc.NewSessionService(repo)
	h := sessionHandler.NewSessionHandler(svc, r.auditService())
	pRepo := r.permissionRepo()
	mdw := r.middleware(pRepo)

	// Session management endpoints (authenticated users only)
	sessionGroup := r.App.Group("/api/user").Use(mdw.AuthMiddleware())
	{
		sessionGroup.GET("/sessions", h.GetActiveSessions)
		sessionGroup.DELETE("/session/:session_id", h.RevokeSession)
		sessionGroup.POST("/sessions/revoke-others", h.RevokeAllOtherSessions)
	}

	logger.WriteLog(logger.LogLevelInfo, "Session management routes registered")
}

func (r *Routes) LocationRoutes() {
	repo := locationRepo.NewLocationRepo(r.DB)
	svc := locationSvc.NewLocationService(repo, database.GetRedisClient())
	h := locationHandler.NewLocationHandler(svc)
	pRepo := r.permissionRepo()
	mdw := r.middleware(pRepo)

	location := r.App.Group("/api/location")
	{
		location.GET("/province", h.GetProvince)
		location.GET("/city", h.GetCity)
		location.GET("/district", h.GetDistrict)
		location.GET("/village", h.GetVillage)
	}

	locationPriv := r.App.Group("/api/location").Use(mdw.AuthMiddleware())
	{
		locationPriv.POST("/sync", mdw.PermissionMiddleware("locations", "sync"), h.Sync)
		locationPriv.GET("/sync/:id", mdw.PermissionMiddleware("locations", "sync"), h.GetSyncJob)
	}
}

func (r *Routes) MediaRoutes() error {
	if !utils.GetEnv("MEDIA_ENABLED", false) {
		logger.WriteLog(logger.LogLevelDebug, "Media routes disabled")
		return nil
	}

	storageProvider, err := mediaInfrastructure.InitStorage()
	if err != nil {
		return err
	}

	maxFileSize := int64(utils.GetEnv("MEDIA_MAX_FILE_SIZE_MB", 5)) << 20
	allowedContentTypes := strings.Split(utils.GetEnv("MEDIA_ALLOWED_CONTENT_TYPES", "image/jpeg,image/png,image/webp,image/gif,application/pdf"), ",")
	repo := mediaRepo.NewMediaRepo(r.DB)
	svc := mediaSvc.NewMediaService(repo, storageProvider, maxFileSize, allowedContentTypes)
	h := mediaHandler.NewMediaHandler(svc, r.auditService())
	mdw := r.middleware(r.permissionRepo())

	media := r.App.Group("/api/media").Use(mdw.AuthMiddleware())
	{
		uploadLimiter := middlewares.IPRateLimitMiddleware(
			database.GetRedisClient(),
			"media_upload",
			utils.GetEnv("MEDIA_UPLOAD_RATE_LIMIT", 20),
			time.Duration(utils.GetEnv("MEDIA_UPLOAD_RATE_WINDOW_SECONDS", 60))*time.Second,
		)
		media.POST("", uploadLimiter, h.Upload)
		media.DELETE("/:id", h.Delete)
	}

	logger.WriteLog(logger.LogLevelInfo, "Media routes registered")
	return nil
}
