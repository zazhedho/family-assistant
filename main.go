package main

import (
	"context"
	"database/sql"
	"errors"
	"family-assistant/infrastructure/database"
	notificationInfra "family-assistant/infrastructure/notification"
	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	mcpHandler "family-assistant/internal/handlers/mcp"
	interfacenotification "family-assistant/internal/interfaces/notification"
	interfaceonboarding "family-assistant/internal/interfaces/onboarding"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	activityRepo "family-assistant/internal/repositories/activity"
	identityRepo "family-assistant/internal/repositories/identity"
	invitationRepo "family-assistant/internal/repositories/invitation"
	onboardingRepo "family-assistant/internal/repositories/onboarding"
	permissionRepo "family-assistant/internal/repositories/permission"
	reminderRepo "family-assistant/internal/repositories/reminder"
	roleRepo "family-assistant/internal/repositories/role"
	spaceRepo "family-assistant/internal/repositories/space"
	userRepo "family-assistant/internal/repositories/user"
	"family-assistant/internal/router"
	activityService "family-assistant/internal/services/activity"
	authorizationService "family-assistant/internal/services/authorization"
	identityService "family-assistant/internal/services/identity"
	invitationService "family-assistant/internal/services/invitation"
	onboardingService "family-assistant/internal/services/onboarding"
	permissionService "family-assistant/internal/services/permission"
	reminderService "family-assistant/internal/services/reminder"
	spaceService "family-assistant/internal/services/space"
	"family-assistant/pkg/config"
	"family-assistant/pkg/logger"
	"family-assistant/utils"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // Register the PostgreSQL driver for migrations.
	_ "github.com/golang-migrate/migrate/v4/source/file"       // Register the file-based migration source.
	"github.com/joho/godotenv"
)

func FailOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configureRuntime()

	var (
		err        error
		sqlDb      *sql.DB
		runMigrate bool
	)

	var port, appName string
	flag.StringVar(&port, "port", os.Getenv("PORT"), "port of the service")
	flag.StringVar(&appName, "appname", os.Getenv("APP_NAME"), "service name")
	flag.BoolVar(&runMigrate, "migrate", utils.GetEnv("RUN_MIGRATION", true), "run database migration before starting server")
	flag.Parse()
	logger.WriteLog(logger.LogLevelInfo, "APP: "+appName+"; PORT: "+port)

	confID := config.GetAppConf("CONFIG_ID", "", nil)
	logger.WriteLog(logger.LogLevelDebug, fmt.Sprintf("ConfigID: %s", confID))
	mcpConfig := config.LoadMCPConfig()
	FailOnError(config.ValidateStartupConfig(port), "Invalid app configuration")

	if runMigrate {
		runMigration()
	}

	redisEnabled, closeRedis := initializeRedis()
	defer closeRedis()

	routes := router.NewRoutes()

	routes.DB, sqlDb, err = database.ConnDb()
	FailOnError(err, "Failed to open db")
	defer closeDatabase(sqlDb)

	routes.UserRoutes()
	routes.RoleRoutes()
	routes.PermissionRoutes()
	routes.MenuRoutes()
	routes.AppConfigRoutes()
	routes.AuditRoutes()
	routes.LocationRoutes()
	FailOnError(routes.MediaRoutes(), "Failed to initialize media routes")

	spaceRepository := spaceRepo.NewRepository(routes.DB)
	roleRepository := roleRepo.NewRoleRepo(routes.DB)
	permissionRepository := permissionRepo.NewPermissionRepo(routes.DB)
	permissions := permissionService.NewPermissionService(permissionRepository)
	audit := routes.AuditService()
	spaces := spaceService.NewService(spaceRepository, roleRepository, permissionRepository, audit)
	invitations := invitationService.NewService(
		invitationRepo.NewRepository(routes.DB), spaceRepository, roleRepository, permissionRepository, audit, config.LoadInvitationConfig(),
	)
	identityRepository := identityRepo.NewRepository(routes.DB)
	identityLink := identityService.NewLinkService(identityRepository, audit, config.LoadIdentityConfig())
	var accountRegistrar interfaceonboarding.ServiceOnboardingInterface = onboardingService.NewService(onboardingRepo.NewRepository(routes.DB), roleRepository, audit)
	identityResolver := identityService.NewResolver(identityRepository, spaceRepository, permissions)
	reminderRepository := reminderRepo.NewRepository(routes.DB)
	reminderSchedulerRepository := reminderRepo.NewSchedulerRepository(routes.DB)
	reminders := reminderService.NewReminderService(reminderRepository, spaceRepository, authorizationService.NewAuthorizer(), audit)
	activities := activityService.NewService(
		activityRepo.NewRepository(routes.DB), spaceRepository, authorizationService.NewAuthorizer(), audit,
	)
	routes.SpaceRoutesWithDependencies(spaces, invitations, userRepo.NewUserRepo(routes.DB), identityLink)
	routes.ReminderRoutes(reminders, identityResolver, permissions)

	// Register session routes if Redis is available
	if redisEnabled {
		routes.SessionRoutes()
	}

	logger.WriteLog(logger.LogLevelInfo, "All routes registered successfully")

	serverContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if _, err := startReminderScheduler(serverContext, config.LoadReminderSchedulerConfig(), reminderSchedulerRepository, identityRepository, spaceRepository); err != nil {
		return fmt.Errorf("start reminder scheduler: %w", err)
	}

	mcpServer, mcpListener := startMCPServer(mcpConfig.Enabled, func() (*http.Server, net.Listener, error) {
		return mcpHandler.Listen(mcpConfig, identityResolver, identityLink, accountRegistrar, spaces, reminders, mcpHandler.ToolServices{
			Invitation: invitations,
			Activity:   activities,
		})
	})
	httpServer, httpListener := bindHTTPServer(serverContext, fmt.Sprintf(":%s", port), routes.App, mcpListener)

	if mcpServer != nil {
		logger.WriteLog(logger.LogLevelInfo, "MCP server listening on "+mcpServer.Addr)
	}
	logger.WriteLog(logger.LogLevelInfo, "All servers listening")

	if err = runServerLifecycle(serverContext, httpServer, httpListener, mcpServer, mcpListener); err != nil {
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	}
	return nil
}

type schedulerIdentityLookup interface {
	FindActiveByUserID(context.Context, string, string) (*domainidentity.ExternalIdentity, error)
}

type schedulerMembershipLookup interface {
	ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error)
}

func startReminderScheduler(
	ctx context.Context,
	cfg config.ReminderSchedulerConfig,
	reminders interfacereminder.SchedulerRepository,
	identities schedulerIdentityLookup,
	members schedulerMembershipLookup,
) (*reminderService.ReminderScheduler, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	sender, err := notificationInfra.NewWhatsAppBridge(cfg.WhatsAppBridgeURL)
	if err != nil {
		return nil, err
	}
	scheduler := reminderService.NewReminderScheduler(
		reminders, identities, members,
		map[string]interfacenotification.NotificationSender{"whatsapp": sender, "whatsapp_cloud": sender},
		cfg.Interval, cfg.Lease, cfg.BatchSize,
	)
	go scheduler.Run(ctx)
	return scheduler, nil
}

func configureRuntime() {
	if timeZone, err := time.LoadLocation("Asia/Jakarta"); err != nil {
		logger.WriteLog(logger.LogLevelError, "time.LoadLocation - Error: "+err.Error())
	} else {
		time.Local = timeZone
	}

	if err := godotenv.Load(".env"); err != nil && os.Getenv("APP_ENV") == "" {
		log.Fatalf("Error app environment")
	}

	addrs, _ := net.InterfaceAddrs()
	myAddr := findIPv4Address(addrs)
	myAddr += strings.Repeat(" ", 15-len(myAddr))
	FailOnError(os.Setenv("ServerIP", myAddr), "Failed to set server IP")
	logger.WriteLog(logger.LogLevelInfo, "Server IP: "+myAddr)
}

func findIPv4Address(addrs []net.Addr) string {
	for _, address := range addrs {
		ipNet, ok := address.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		return ipNet.IP.String()
	}
	return "unknown"
}

func initializeRedis() (bool, func()) {
	if _, err := database.InitRedis(); err != nil {
		logger.WriteLog(logger.LogLevelDebug, "Redis not available, session management will be disabled")
		return false, func() {
			// No Redis client was created, so there is nothing to close.
		}
	}
	logger.WriteLog(logger.LogLevelInfo, "Redis initialized, session management enabled")
	return true, closeRedis
}

func closeRedis() {
	if err := database.CloseRedis(); err != nil {
		logger.WriteLog(logger.LogLevelError, "Failed to close redis connection: "+err.Error())
	}
}

func closeDatabase(db *sql.DB) {
	if err := db.Close(); err != nil {
		logger.WriteLog(logger.LogLevelError, "Failed to close database connection: "+err.Error())
	}
}

func startMCPServer(enabled bool, listen func() (*http.Server, net.Listener, error)) (*http.Server, net.Listener) {
	if !enabled {
		return nil, nil
	}
	server, listener, err := listen()
	FailOnError(err, "Failed to bind MCP server")
	return server, listener
}

func bindHTTPServer(ctx context.Context, addr string, handler http.Handler, mcpListener net.Listener) (*http.Server, net.Listener) {
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.Addr)
	if err != nil {
		if mcpListener != nil {
			_ = mcpListener.Close()
		}
		FailOnError(err, "Failed to bind HTTP server")
	}
	return server, listener
}

const serverShutdownTimeout = 5 * time.Second

type serverEndpoint struct {
	server   *http.Server
	listener net.Listener
}

func serveServers(ctx context.Context, httpServer *http.Server, httpListener net.Listener, mcpServer *http.Server, mcpListener net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoints := []serverEndpoint{{server: httpServer, listener: httpListener}}
	if mcpServer != nil || mcpListener != nil {
		endpoints = append(endpoints, serverEndpoint{server: mcpServer, listener: mcpListener})
	}
	for _, endpoint := range endpoints {
		if endpoint.server == nil || endpoint.listener == nil {
			return errors.New("server listener is not configured")
		}
	}

	results := make(chan error, len(endpoints))
	for _, endpoint := range endpoints {
		go func(endpoint serverEndpoint) {
			results <- endpoint.server.Serve(endpoint.listener)
		}(endpoint)
	}

	select {
	case <-ctx.Done():
		return shutdownServers(endpoints, results, len(endpoints))
	case firstResult := <-results:
		shutdownErr := shutdownServers(endpoints, results, len(endpoints)-1)
		if !expectedServeError(firstResult) {
			return firstResult
		}
		return shutdownErr
	}
}

func runServerLifecycle(ctx context.Context, httpServer *http.Server, httpListener net.Listener, mcpServer *http.Server, mcpListener net.Listener) error {
	return serveServers(ctx, httpServer, httpListener, mcpServer, mcpListener)
}

func shutdownServers(endpoints []serverEndpoint, results <-chan error, remaining int) error {
	shutdownContext, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()

	shutdownErrors := make(chan error, len(endpoints))
	for _, endpoint := range endpoints {
		go func(server *http.Server) {
			shutdownErrors <- server.Shutdown(shutdownContext)
		}(endpoint.server)
	}

	var shutdownErr error
	for range endpoints {
		if err := <-shutdownErrors; err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}

	for ; remaining > 0; remaining-- {
		select {
		case err := <-results:
			if !expectedServeError(err) && shutdownErr == nil {
				shutdownErr = err
			}
		case <-time.After(time.Second):
			return shutdownErr
		}
	}
	return shutdownErr
}

func expectedServeError(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed)
}

func runMigration() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			utils.GetEnv("DB_USERNAME", ""),
			utils.GetEnv("DB_PASS", ""),
			utils.GetEnv("DB_HOST", ""),
			utils.GetEnv("DB_PORT", ""),
			utils.GetEnv("DB_NAME", ""),
			utils.GetEnv("DB_SSLMODE", "disable"))
	}

	m, err := migrate.New(utils.GetEnv("PATH_MIGRATE", "file://migrations"), dsn)
	if err != nil {
		log.Fatal(err)
	}

	if err := m.Up(); err != nil && err.Error() != "no change" {
		log.Fatal(err)
	}
	logger.WriteLog(logger.LogLevelInfo, "Migration Success")
}
