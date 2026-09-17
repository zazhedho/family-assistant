package mcp

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
	"github.com/zazhedho/family-assistant/pkg/config"
)

func TestListenPropagatesBindErrorSynchronously(t *testing.T) {
	errAddressInUse := errors.New("address already in use")
	server, bound, err := listenMCP(config.MCPConfig{
		Addr:      "127.0.0.1:8081",
		ServerKey: "secret",
	}, nil, func(string, string) (net.Listener, error) {
		return nil, errAddressInUse
	})
	if !errors.Is(err, errAddressInUse) {
		t.Fatalf("expected address-in-use bind error, got %v", err)
	}
	if server != nil || bound != nil {
		t.Fatalf("failed bind returned server/listener: server=%v listener=%v", server, bound)
	}
}

func TestHTTPServerUsesSafeTimeoutsWithoutWriteTimeout(t *testing.T) {
	server := NewHTTPServer(config.MCPConfig{Addr: "127.0.0.1:0"}, nil)
	if server.ReadHeaderTimeout <= 0 {
		t.Fatal("expected MCP ReadHeaderTimeout")
	}
	if server.IdleTimeout <= 0 {
		t.Fatal("expected MCP IdleTimeout")
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("streaming MCP server must not set WriteTimeout, got %s", server.WriteTimeout)
	}
}

func TestHTTPHandlerMountsMCPOnlyAtExactPath(t *testing.T) {
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, &resolverStub{
		actor: serviceidentity.ActorContext{
			UserID:   "user-1",
			MemberID: "member-1",
			FamilyID: "family-1",
			RoleName: "parent",
		},
	})

	for _, path := range []string{"/", "/other", "/mcp/"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("path %s: expected 404, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("MCP path should reach authentication middleware, got %d: %s", rec.Code, rec.Body.String())
	}
}
