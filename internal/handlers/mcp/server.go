package mcp

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
	"github.com/zazhedho/family-assistant/pkg/config"
)

func NewHTTPHandler(cfg config.MCPConfig, resolver serviceidentity.Resolver) http.Handler {
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

func NewHTTPServer(cfg config.MCPConfig, resolver serviceidentity.Resolver) *http.Server {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	return &http.Server{
		Addr:              addr,
		Handler:           NewHTTPHandler(cfg, resolver),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func NewServer(cfg config.MCPConfig, resolver serviceidentity.Resolver) *http.Server {
	return NewHTTPServer(cfg, resolver)
}

// Listen binds the MCP address before returning, so startup can fail before
// advertising readiness when the address is unavailable.
func Listen(cfg config.MCPConfig, resolver serviceidentity.Resolver) (*http.Server, net.Listener, error) {
	return listenMCP(cfg, resolver, net.Listen)
}

func listenMCP(cfg config.MCPConfig, resolver serviceidentity.Resolver, listen func(string, string) (net.Listener, error)) (*http.Server, net.Listener, error) {
	server := NewHTTPServer(cfg, resolver)
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, err
	}
	return server, listener, nil
}
