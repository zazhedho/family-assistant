package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"github.com/zazhedho/family-assistant/infrastructure/database"
	mcpHandler "github.com/zazhedho/family-assistant/internal/handlers/mcp"
	familyMemberRepo "github.com/zazhedho/family-assistant/internal/repositories/familymember"
	permissionRepo "github.com/zazhedho/family-assistant/internal/repositories/permission"
	reminderRepo "github.com/zazhedho/family-assistant/internal/repositories/reminder"
	"github.com/zazhedho/family-assistant/internal/router"
	authorizationService "github.com/zazhedho/family-assistant/internal/services/authorization"
	identityService "github.com/zazhedho/family-assistant/internal/services/identity"
	permissionService "github.com/zazhedho/family-assistant/internal/services/permission"
	reminderService "github.com/zazhedho/family-assistant/internal/services/reminder"
	"github.com/zazhedho/family-assistant/pkg/config"
	"github.com/zazhedho/family-assistant/pkg/logger"
	"github.com/zazhedho/family-assistant/utils"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
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
	var (
		err        error
		sqlDb      *sql.DB
		runMigrate bool
	)
	if timeZone, err := time.LoadLocation("Asia/Jakarta"); err != nil {
		logger.WriteLog(logger.LogLevelError, "time.LoadLocation - Error: "+err.Error())
	} else {
		time.Local = timeZone
	}

	if err = godotenv.Load(".env"); err != nil && os.Getenv("APP_ENV") == "" {
		log.Fatalf("Error app environment")
	}

	myAddr := "unknown"
	addrs, _ := net.InterfaceAddrs()
	for _, address := range addrs {
		if ipNet, ok := address.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				myAddr = ipNet.IP.String()
				break
			}
		}
	}

	myAddr += strings.Repeat(" ", 15-len(myAddr))
	FailOnError(os.Setenv("ServerIP", myAddr), "Failed to set server IP")
	logger.WriteLog(logger.LogLevelInfo, "Server IP: "+myAddr)

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

	// Initialize Redis for session management (optional)
	redisClient, err := database.InitRedis()
	if err != nil {
		logger.WriteLog(logger.LogLevelDebug, "Redis not available, session management will be disabled")
	} else {
		defer func() {
			if closeErr := database.CloseRedis(); closeErr != nil {
				logger.WriteLog(logger.LogLevelError, "Failed to close redis connection: "+closeErr.Error())
			}
		}()
		logger.WriteLog(logger.LogLevelInfo, "Redis initialized, session management enabled")
	}

	routes := router.NewRoutes()

	routes.DB, sqlDb, err = database.ConnDb()
	FailOnError(err, "Failed to open db")
	defer func() {
		if closeErr := sqlDb.Close(); closeErr != nil {
			logger.WriteLog(logger.LogLevelError, "Failed to close database connection: "+closeErr.Error())
		}
	}()

	routes.UserRoutes()
	routes.RoleRoutes()
	routes.PermissionRoutes()
	routes.MenuRoutes()
	routes.AppConfigRoutes()
	routes.AuditRoutes()
	routes.LocationRoutes()
	FailOnError(routes.MediaRoutes(), "Failed to initialize media routes")

	familyMembers := familyMemberRepo.NewRepository(routes.DB)
	permissions := permissionService.NewPermissionService(permissionRepo.NewPermissionRepo(routes.DB))
	identityResolver := identityService.NewResolver(familyMembers, permissions)
	reminders := reminderService.NewReminderService(
		reminderRepo.NewRepository(routes.DB), familyMembers, authorizationService.NewAuthorizer(), routes.AuditService(),
	)
	routes.ReminderRoutes(reminders, identityResolver)

	// Register session routes if Redis is available
	if redisClient != nil {
		routes.SessionRoutes()
	}

	logger.WriteLog(logger.LogLevelInfo, "All routes registered successfully")

	serverContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	var mcpServer *http.Server
	var mcpListener net.Listener
	if mcpConfig.Enabled {
		mcpServer, mcpListener, err = mcpHandler.Listen(mcpConfig, identityResolver, reminders)
		FailOnError(err, "Failed to bind MCP server")
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%s", port),
		Handler:           routes.App,
		ReadHeaderTimeout: 10 * time.Second,
	}
	httpListener, err := (&net.ListenConfig{}).Listen(serverContext, "tcp", httpServer.Addr)
	if err != nil {
		if mcpListener != nil {
			_ = mcpListener.Close()
		}
		FailOnError(err, "Failed to bind HTTP server")
	}

	if mcpServer != nil {
		logger.WriteLog(logger.LogLevelInfo, "MCP server listening on "+mcpServer.Addr)
	}
	logger.WriteLog(logger.LogLevelInfo, "All servers listening")

	if err = runServerLifecycle(serverContext, httpServer, httpListener, mcpServer, mcpListener); err != nil {
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	}
	return nil
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
