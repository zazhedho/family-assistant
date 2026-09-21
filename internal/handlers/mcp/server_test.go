package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/config"
)

func TestListenPropagatesBindErrorSynchronously(t *testing.T) {
	errAddressInUse := errors.New("address already in use")
	server, bound, err := listenMCP(config.MCPConfig{
		Addr:      "127.0.0.1:8081",
		ServerKey: "secret",
	}, nil, nil, nil, nil, nil, func(string, string) (net.Listener, error) {
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
	server := NewHTTPServer(config.MCPConfig{Addr: "127.0.0.1:0"}, nil, nil, nil, nil, nil)
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
		actor: domainidentity.ActorContext{
			UserID:   "user-1",
			MemberID: "member-1",
			Source:   "mcp",
			RoleName: "parent",
		},
	}, nil, nil, nil, nil)

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

type mcpServerResponse struct {
	Result *struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		StructuredOutput json.RawMessage `json:"structuredContent"`
		IsError          bool            `json:"isError"`
		Tools            []struct {
			Name         string          `json:"name"`
			OutputSchema json.RawMessage `json:"outputSchema"`
		} `json:"tools"`
	} `json:"result,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func callMCPServer(t *testing.T, serverURL, profile, key, method string, params map[string]any) mcpServerResponse {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		t.Fatalf("marshal MCP request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/mcp", bytes.NewReader(payload)) // #nosec G704 -- local httptest server.
	if err != nil {
		t.Fatalf("build MCP request: %v", err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Hermes-Profile", profile)
	response, err := http.DefaultClient.Do(req) // #nosec G704 -- local httptest server.
	if err != nil {
		t.Fatalf("MCP request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read MCP response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("MCP %s status/body = %d/%s", method, response.StatusCode, body)
	}
	var decoded mcpServerResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode MCP %s response %s: %v", method, body, err)
	}
	return decoded
}

func mcpHTTPStatus(t *testing.T, serverURL, profile, key, method string, params map[string]any) int {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		t.Fatalf("marshal MCP request: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/mcp", bytes.NewReader(payload)) // #nosec G704 -- local httptest server.
	if err != nil {
		t.Fatalf("build MCP request: %v", err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Hermes-Profile", profile)
	response, err := http.DefaultClient.Do(req) // #nosec G704 -- local httptest server.
	if err != nil {
		t.Fatalf("MCP request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

func initializeMCPServer(t *testing.T, serverURL string) {
	t.Helper()
	response := callMCPServer(t, serverURL, "new-profile", "secret", "initialize", map[string]any{
		"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "mcp-test", "version": "1"},
	})
	if response.Error != nil {
		t.Fatalf("initialize error: %s", response.Error.Message)
	}
}

func TestHTTPHandlerIdentityLinkNeedsOnlyServerAuthentication(t *testing.T) {
	linkService := &identityLinkServiceStub{identity: &domainidentity.ExternalIdentity{
		ID: "identity-1", UserID: "user-1", Provider: domainidentity.ProviderHermes,
		ExternalID: "new-profile", Status: domainidentity.StatusActive,
	}}
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, &resolverStub{err: serviceidentity.ErrUnauthenticated}, linkService, nil, nil, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	initializeMCPServer(t, server.URL)
	response := callMCPServer(t, server.URL, "new-profile", "secret", "tools/call", map[string]any{
		"name": "identity_link", "arguments": map[string]any{"code": "ABC123"},
	})
	if response.Error != nil || response.Result == nil || response.Result.IsError {
		t.Fatalf("identity_link failed: %+v", response)
	}
	if linkService.calls != 1 || linkService.externalID != "new-profile" {
		t.Fatalf("unexpected link call: %+v", linkService)
	}
}

func TestHTTPHandlerProtectedToolRejectsUnlinkedProfile(t *testing.T) {
	resolver := &resolverStub{err: serviceidentity.ErrUnauthenticated}
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, resolver, nil, nil, nil, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	initializeMCPServer(t, server.URL)
	response := callMCPServer(t, server.URL, "new-profile", "secret", "tools/call", map[string]any{
		"name": "space_list", "arguments": map[string]any{},
	})
	if response.Error != nil || response.Result == nil || !response.Result.IsError {
		t.Fatalf("space_list should reject unlinked profile: %+v", response)
	}
	if len(response.Result.Content) == 0 || !strings.Contains(response.Result.Content[0].Text, "authentication required") {
		t.Fatalf("space_list leaked or omitted safe auth error: %+v", response.Result.Content)
	}
	if resolver.resolveCall != 1 {
		t.Fatalf("expected one protected resolver call, got %d", resolver.resolveCall)
	}
}

func TestHTTPHandlerExposesOnlyCurrentMCPToolsWhenRemindersAreAbsent(t *testing.T) {
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, &resolverStub{}, nil, nil, nil, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	initializeMCPServer(t, server.URL)
	response := callMCPServer(t, server.URL, "new-profile", "secret", "tools/list", map[string]any{})
	if response.Error != nil || response.Result == nil {
		t.Fatalf("tools/list failed: %+v", response)
	}
	got := make([]string, 0, len(response.Result.Tools))
	for _, tool := range response.Result.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	want := []string{"account_register", "identity_link", "reminder_complete", "reminder_create", "reminder_list", "space_get_members", "space_list"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", got, want)
	}
}

func TestHTTPHandlerToolOutputSchemasUseHermesObjectRoot(t *testing.T) {
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, &resolverStub{}, nil, nil, nil, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	initializeMCPServer(t, server.URL)
	response := callMCPServer(t, server.URL, "profile", "secret", "tools/list", map[string]any{})
	if response.Error != nil || response.Result == nil {
		t.Fatalf("tools/list failed: %+v", response)
	}
	for _, tool := range response.Result.Tools {
		var schema struct {
			Type json.RawMessage `json:"type"`
		}
		if err := json.Unmarshal(tool.OutputSchema, &schema); err != nil {
			t.Fatalf("decode %s output schema: %v", tool.Name, err)
		}
		if string(schema.Type) != `"object"` {
			t.Errorf("%s output schema type = %q, want object: %s", tool.Name, schema.Type, tool.OutputSchema)
		}
	}
}

func TestHTTPHandlerAccountRegisterNeedsServerAuthenticationAndTrustedProfile(t *testing.T) {
	registrar := &registrarStub{result: dto.AccountRegistrationResult{Status: "created", UserID: "user-1", SpaceID: "space-1"}}
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, &resolverStub{err: serviceidentity.ErrUnauthenticated}, nil, registrar, nil, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	params := map[string]any{
		"name": "account_register", "arguments": map[string]any{
			"name": "Jane Doe", "birth_date": "1990-05-20", "consent": true,
		},
	}
	if status := mcpHTTPStatus(t, server.URL, "profile-1", "wrong", "tools/call", params); status != http.StatusUnauthorized {
		t.Fatalf("wrong server key status = %d, want %d", status, http.StatusUnauthorized)
	}
	if status := mcpHTTPStatus(t, server.URL, "", "secret", "tools/call", params); status != http.StatusUnauthorized {
		t.Fatalf("missing trusted profile status = %d, want %d", status, http.StatusUnauthorized)
	}

	initializeMCPServer(t, server.URL)
	response := callMCPServer(t, server.URL, "profile-1", "secret", "tools/call", params)
	if response.Error != nil || response.Result == nil || response.Result.IsError {
		t.Fatalf("account_register failed without linked actor: %+v", response)
	}
	if registrar.calls != 1 || registrar.input.Provider != "hermes" || registrar.input.ExternalID != "profile-1" {
		t.Fatalf("registrar input = %+v, calls = %d", registrar.input, registrar.calls)
	}
}

func TestHTTPHandlerProtectedSpaceToolMapsSuccessForbiddenAndNotFound(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000201"
	expectedMember := domainspace.ResolvedMembership{
		ID: "member-1", SpaceID: spaceID, SpaceName: "Jane", SpaceType: domainspace.TypePersonal,
		UserID: "user-1", RoleID: "role-owner", RoleName: "space_owner", Status: domainspace.StatusActive,
	}
	resolver := &resolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{{
			ID: "00000000-0000-0000-0000-000000000301", SpaceID: spaceID, SpaceName: "Jane", SpaceType: domainspace.TypePersonal,
			UserID: "user-1", RoleID: "role-owner", RoleName: "space_owner", Status: domainspace.StatusActive,
		}}},
		permissions: []domainpermission.Permission{{Resource: "members", Action: "list"}},
	}
	for _, tt := range []struct {
		name    string
		members []domainspace.ResolvedMembership
		err     error
		want    string
	}{
		{name: "success", members: []domainspace.ResolvedMembership{expectedMember}},
		{name: "forbidden", err: serviceauthorization.ErrForbidden, want: "forbidden"},
		{name: "not found", err: serviceauthorization.ErrNotFound, want: "not found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver.resolveCall = 0
			service := &mcpSpaceServiceStub{members: tt.members, membersErr: tt.err}
			handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, resolver, nil, nil, service, nil)
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)
			initializeMCPServer(t, server.URL)
			response := callMCPServer(t, server.URL, "profile", "secret", "tools/call", map[string]any{
				"name": "space_get_members", "arguments": map[string]any{"space": spaceID},
			})
			if tt.err == nil {
				if response.Error != nil || response.Result == nil || response.Result.IsError || len(response.Result.StructuredOutput) == 0 {
					t.Fatalf("success response = %+v", response)
				}
				if resolver.resolveCall != 1 || resolver.profileID != "profile" || resolver.channel != "whatsapp" {
					t.Fatalf("resolver identity = calls:%d profile:%q channel:%q, want one trusted profile call", resolver.resolveCall, resolver.profileID, resolver.channel)
				}
				if service.membersCalls != 1 || service.membersUser != "user-1" || service.membersSpace != spaceID {
					t.Fatalf("space service args = calls:%d user:%q space:%q, want one call for trusted user and selected Space", service.membersCalls, service.membersUser, service.membersSpace)
				}
				var payload struct {
					Members []domainspace.ResolvedMembership `json:"members"`
				}
				if err := json.Unmarshal(response.Result.StructuredOutput, &payload); err != nil {
					t.Fatalf("decode success structured output: %v", err)
				}
				if len(payload.Members) != 1 || payload.Members[0] != expectedMember {
					t.Fatalf("success payload = %+v, want %+v", payload.Members, []domainspace.ResolvedMembership{expectedMember})
				}
				return
			}
			if response.Error != nil || response.Result == nil || !response.Result.IsError || len(response.Result.Content) == 0 || !strings.Contains(response.Result.Content[0].Text, tt.want) {
				t.Fatalf("%s response = %+v", tt.name, response)
			}
		})
	}
}
