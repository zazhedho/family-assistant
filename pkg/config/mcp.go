package config

import (
	"errors"
	"strings"

	"github.com/zazhedho/family-assistant/utils"
)

type MCPConfig struct {
	Enabled       bool
	Addr          string
	ServerKey     string
	ProfileHeader string
}

func LoadMCPConfig() MCPConfig {
	addr := strings.TrimSpace(utils.GetEnv("MCP_ADDR", ""))
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	profileHeader := strings.TrimSpace(utils.GetEnv("MCP_PROFILE_HEADER", ""))
	if profileHeader == "" {
		profileHeader = "X-Hermes-Profile"
	}

	return MCPConfig{
		Enabled:       utils.GetEnv("MCP_ENABLED", false),
		Addr:          addr,
		ServerKey:     strings.TrimSpace(utils.GetEnv("MCP_SERVER_KEY", "")),
		ProfileHeader: profileHeader,
	}
}

func ValidateMCPConfig() error {
	if problems := validateMCPConfig(); len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validateMCPConfig() []string {
	conf := LoadMCPConfig()
	if conf.Enabled && conf.ServerKey == "" {
		return []string{"MCP_SERVER_KEY is required when MCP_ENABLED=true"}
	}
	return nil
}
