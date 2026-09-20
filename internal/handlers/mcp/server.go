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

func NewHTTPHandler(cfg config.MCPConfig, resolver serviceidentity.Resolver, linkService serviceidentity.LinkService, spaceService servicespace.Service, reminderService servicereminder.Service) http.Handler {
	sdkServer := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "family-assistant",
		Version: "1.0.0",
	}, nil)

	registerIdentityTools(sdkServer, linkService)
	registerSpaceTools(sdkServer, resolver, spaceService)
	registerReminderTools(sdkServer, reminderService, resolver)

	sdkHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return sdkServer
	}, &mcpsdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", NewAuthMiddleware(cfg).Handler(sdkHandler))
	return mux
}

func NewHTTPServer(cfg config.MCPConfig, resolver serviceidentity.Resolver, linkService serviceidentity.LinkService, spaceService servicespace.Service, reminderService servicereminder.Service) *http.Server {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	return &http.Server{
		Addr:              addr,
		Handler:           NewHTTPHandler(cfg, resolver, linkService, spaceService, reminderService),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func NewServer(cfg config.MCPConfig, resolver serviceidentity.Resolver, linkService serviceidentity.LinkService, spaceService servicespace.Service, reminderService servicereminder.Service) *http.Server {
	return NewHTTPServer(cfg, resolver, linkService, spaceService, reminderService)
}

// Listen binds the MCP address before returning, so startup can fail before
// advertising readiness when the address is unavailable.
func Listen(cfg config.MCPConfig, resolver serviceidentity.Resolver, linkService serviceidentity.LinkService, spaceService servicespace.Service, reminderService servicereminder.Service) (*http.Server, net.Listener, error) {
	return listenMCPWithDependencies(cfg, resolver, linkService, spaceService, reminderService, net.Listen)
}

func listenMCP(cfg config.MCPConfig, resolver serviceidentity.Resolver, linkService serviceidentity.LinkService, spaceService servicespace.Service, reminderService servicereminder.Service, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	return listenMCPWithDependencies(cfg, resolver, linkService, spaceService, reminderService, listen)
}

func listenMCPWithDependencies(cfg config.MCPConfig, resolver serviceidentity.Resolver, linkService serviceidentity.LinkService, spaceService servicespace.Service, reminderService servicereminder.Service, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	server := NewHTTPServer(cfg, resolver, linkService, spaceService, reminderService)
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, err
	}
	return server, listener, nil
}
