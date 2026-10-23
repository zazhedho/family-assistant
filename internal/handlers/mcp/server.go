package mcp

import (
	"net"
	"net/http"
	"strings"
	"time"

	interfaceactivity "family-assistant/internal/interfaces/activity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	interfaceonboarding "family-assistant/internal/interfaces/onboarding"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	interfacespace "family-assistant/internal/interfaces/space"
	"family-assistant/pkg/config"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolServices struct {
	Invitation       interfaceinvitation.ServiceInvitationInterface
	Activity         interfaceactivity.ServiceActivityInterface
	ScheduledMessage interfacescheduledmessage.Service
	// GroupBinder backs space_channel_bind, which is registered whether or not the
	// scheduled-message lane is enabled; when nil the tool fails closed.
	GroupBinder interfacescheduledmessage.GroupBinder
	// GroupSpaces resolves the Space bound to a WhatsApp group; without it a blank
	// space selector in a group fails closed.
	GroupSpaces interfaceidentity.GroupSpaceLookup
}

type mcpDependencies struct {
	resolver         interfaceidentity.Resolver
	linkService      interfaceidentity.LinkService
	accountRegistrar interfaceonboarding.ServiceOnboardingInterface
	spaceService     interfacespace.ServiceSpaceInterface
	reminderService  interfacereminder.ServiceReminderInterface
	toolServices     ToolServices
}

func NewHTTPHandler(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface, toolServices ToolServices) http.Handler {
	sdkServer := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "family-assistant",
		Version: "1.0.0",
	}, nil)

	registerIdentityTools(sdkServer, resolver, linkService)
	registerAccountTools(sdkServer, accountRegistrar)
	registerSpaceTools(sdkServer, resolver, spaceService)
	registerReminderTools(sdkServer, reminderService, resolver)
	registerInvitationTools(sdkServer, resolver, toolServices.Invitation)
	registerActivityTools(sdkServer, resolver, toolServices.Activity, reminderService)
	registerGroupBindTool(sdkServer, resolver, toolServices.GroupBinder)
	if toolServices.ScheduledMessage != nil {
		registerScheduledMessageTools(sdkServer, resolver, toolServices.ScheduledMessage)
	}

	sdkHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return sdkServer
	}, &mcpsdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", NewAuthMiddleware(cfg).Handler(groupSpaceLookupHandler(toolServices.GroupSpaces, sdkHandler)))
	return mux
}

func NewHTTPServer(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface, toolServices ToolServices) *http.Server {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	return &http.Server{
		Addr:              addr,
		Handler:           NewHTTPHandler(cfg, resolver, linkService, accountRegistrar, spaceService, reminderService, toolServices),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// Listen binds the MCP address before returning, so startup can fail before
// advertising readiness when the address is unavailable.
func Listen(cfg config.MCPConfig, resolver interfaceidentity.Resolver, linkService interfaceidentity.LinkService, accountRegistrar interfaceonboarding.ServiceOnboardingInterface, spaceService interfacespace.ServiceSpaceInterface, reminderService interfacereminder.ServiceReminderInterface, toolServices ToolServices) (*http.Server, net.Listener, error) {
	return listenMCP(cfg, mcpDependencies{
		resolver:         resolver,
		linkService:      linkService,
		accountRegistrar: accountRegistrar,
		spaceService:     spaceService,
		reminderService:  reminderService,
		toolServices:     toolServices,
	}, net.Listen)
}

func listenMCP(cfg config.MCPConfig, dependencies mcpDependencies, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	server := NewHTTPServer(cfg, dependencies.resolver, dependencies.linkService, dependencies.accountRegistrar, dependencies.spaceService, dependencies.reminderService, dependencies.toolServices)
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, err
	}
	return server, listener, nil
}

// groupSpaceLookupHandler exposes the group Space lookup to tool handlers through
// the request context, next to the identity verifier set by the auth middleware.
func groupSpaceLookupHandler(lookup interfaceidentity.GroupSpaceLookup, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(withGroupSpaceLookup(r.Context(), lookup)))
	})
}
