package mcp

import (
	"net"
	"net/http"
	"strings"
	"time"

	serviceidentity "family-assistant/internal/services/identity"
	servicereminder "family-assistant/internal/services/reminder"
	servicespace "family-assistant/internal/services/space"
	"family-assistant/pkg/config"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewHTTPHandler(cfg config.MCPConfig, resolver any, dependencies ...any) http.Handler {
	linkService, spaceService, reminderService := mcpDependencies(dependencies)
	sdkServer := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "family-assistant",
		Version: "1.0.0",
	}, nil)

	registerIdentityTools(sdkServer, linkService)
	registerSpaceTools(sdkServer, resolver, spaceService)
	if reminderService != nil {
		registerReminderTools(sdkServer, reminderService, resolver)
	}

	sdkHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return sdkServer
	}, &mcpsdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", NewAuthMiddleware(cfg, resolver).Handler(sdkHandler))
	return mux
}

func NewHTTPServer(cfg config.MCPConfig, resolver any, dependencies ...any) *http.Server {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	return &http.Server{
		Addr:              addr,
		Handler:           NewHTTPHandler(cfg, resolver, dependencies...),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func NewServer(cfg config.MCPConfig, resolver any, dependencies ...any) *http.Server {
	return NewHTTPServer(cfg, resolver, dependencies...)
}

// Listen binds the MCP address before returning, so startup can fail before
// advertising readiness when the address is unavailable.
func Listen(cfg config.MCPConfig, resolver any, dependencies ...any) (*http.Server, net.Listener, error) {
	return listenMCPWithDependencies(cfg, resolver, dependencies, net.Listen)
}

func listenMCP(cfg config.MCPConfig, resolver any, reminderService servicereminder.Service, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	return listenMCPWithDependencies(cfg, resolver, []any{reminderService}, listen)
}

func listenMCPWithDependencies(cfg config.MCPConfig, resolver any, dependencies []any, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	server := NewHTTPServer(cfg, resolver, dependencies...)
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, err
	}
	return server, listener, nil
}

func mcpDependencies(dependencies []any) (serviceidentity.LinkService, servicespace.Service, servicereminder.Service) {
	var linkService serviceidentity.LinkService
	var spaceService servicespace.Service
	var reminderService servicereminder.Service
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case serviceidentity.LinkService:
			linkService = value
		case servicespace.Service:
			spaceService = value
		case servicereminder.Service:
			reminderService = value
		}
	}
	return linkService, spaceService, reminderService
}
