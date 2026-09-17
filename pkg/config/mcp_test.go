package config

import (
	"strings"
	"testing"
)

func TestLoadMCPConfigUsesSafeDefaults(t *testing.T) {
	for _, key := range []string{"MCP_ENABLED", "MCP_ADDR", "MCP_SERVER_KEY", "MCP_PROFILE_HEADER"} {
		t.Setenv(key, "")
	}

	got := LoadMCPConfig()
	if got.Enabled {
		t.Fatal("expected MCP to be disabled by default")
	}
	if got.Addr != "127.0.0.1:8081" {
		t.Fatalf("expected localhost MCP address, got %q", got.Addr)
	}
	if got.ServerKey != "" {
		t.Fatalf("expected no default MCP server key, got %q", got.ServerKey)
	}
	if got.ProfileHeader != "X-Hermes-Profile" {
		t.Fatalf("expected default profile header, got %q", got.ProfileHeader)
	}
}

func TestValidateMCPConfigRequiresServerKeyWhenEnabled(t *testing.T) {
	t.Setenv("MCP_ENABLED", "true")
	t.Setenv("MCP_SERVER_KEY", " ")

	err := ValidateMCPConfig()
	if err == nil || !strings.Contains(err.Error(), "MCP_SERVER_KEY is required when MCP_ENABLED=true") {
		t.Fatalf("expected missing key validation error, got %v", err)
	}
}

func TestValidateMCPConfigAllowsMissingServerKeyWhenDisabled(t *testing.T) {
	t.Setenv("MCP_ENABLED", "false")
	t.Setenv("MCP_SERVER_KEY", "")

	if err := ValidateMCPConfig(); err != nil {
		t.Fatalf("expected disabled MCP to allow missing key, got %v", err)
	}
}

func TestLoadMCPConfigUsesConfiguredValues(t *testing.T) {
	t.Setenv("MCP_ENABLED", "true")
	t.Setenv("MCP_ADDR", "127.0.0.1:9090")
	t.Setenv("MCP_SERVER_KEY", " mcp-secret ")
	t.Setenv("MCP_PROFILE_HEADER", "X-Trusted-Profile")

	got := LoadMCPConfig()
	if !got.Enabled || got.Addr != "127.0.0.1:9090" || got.ServerKey != "mcp-secret" || got.ProfileHeader != "X-Trusted-Profile" {
		t.Fatalf("unexpected MCP config: %+v", got)
	}
}
