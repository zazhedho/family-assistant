package mcp

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	servicereminder "family-assistant/internal/services/reminder"
	"family-assistant/pkg/config"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewHTTPHandler(cfg config.MCPConfig, resolver any, reminderService servicereminder.Service) http.Handler {
	sdkServer := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "family-assistant",
		Version: "1.0.0",
	}, nil)

	mcpsdk.AddTool(sdkServer, &mcpsdk.Tool{
		Name:        "family_get_member",
		Description: "Return the authenticated family member summary.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, MemberSummary, error) {
		member, err := FamilyGetMember(ctx)
		if err != nil {
			return nil, MemberSummary{}, MapToolError(err)
		}
		return nil, member, nil
	})
	if reminderService != nil {
		registerReminderTools(sdkServer, reminderService)
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

func NewHTTPServer(cfg config.MCPConfig, resolver any, reminderService servicereminder.Service) *http.Server {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	return &http.Server{
		Addr:              addr,
		Handler:           NewHTTPHandler(cfg, resolver, reminderService),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func NewServer(cfg config.MCPConfig, resolver any, reminderService servicereminder.Service) *http.Server {
	return NewHTTPServer(cfg, resolver, reminderService)
}

// Listen binds the MCP address before returning, so startup can fail before
// advertising readiness when the address is unavailable.
func Listen(cfg config.MCPConfig, resolver any, reminderService servicereminder.Service) (*http.Server, net.Listener, error) {
	return listenMCP(cfg, resolver, reminderService, net.Listen)
}

func listenMCP(cfg config.MCPConfig, resolver any, reminderService servicereminder.Service, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	server := NewHTTPServer(cfg, resolver, reminderService)
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, err
	}
	return server, listener, nil
}
