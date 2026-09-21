package mcp

import (
	"net"
	"net/http"
	"strings"
	"time"

	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfaceonboarding "family-assistant/internal/interfaces/onboarding"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	interfacespace "family-assistant/internal/interfaces/space"
	"family-assistant/pkg/config"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewHTTPHandler(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface) http.Handler {
	sdkServer := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "family-assistant",
		Version: "1.0.0",
	}, nil)

	registerIdentityTools(sdkServer, linkService)
	registerAccountTools(sdkServer, accountRegistrar)
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

func NewHTTPServer(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface) *http.Server {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	return &http.Server{
		Addr:              addr,
		Handler:           NewHTTPHandler(cfg, resolver, linkService, accountRegistrar, spaceService, reminderService),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func NewServer(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface) *http.Server {
	return NewHTTPServer(cfg, resolver, linkService, accountRegistrar, spaceService, reminderService)
}

// Listen binds the MCP address before returning, so startup can fail before
// advertising readiness when the address is unavailable.
func Listen(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface) (*http.Server, net.Listener, error) {
	return listenMCPWithDependencies(cfg, resolver, linkService, accountRegistrar, spaceService, reminderService, net.Listen)
}

func listenMCP(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	return listenMCPWithDependencies(cfg, resolver, linkService, accountRegistrar, spaceService, reminderService, listen)
}

func listenMCPWithDependencies(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	server := NewHTTPServer(cfg, resolver, linkService, accountRegistrar, spaceService, reminderService)
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, err
	}
	return server, listener, nil
}
