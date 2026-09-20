package config

import (
	"time"

	"family-assistant/utils"
)

const DefaultIdentityLinkTTL = 10 * time.Minute

const DefaultLinkTokenTTL = DefaultIdentityLinkTTL

type IdentityConfig struct {
	TTL time.Duration
}

func LoadIdentityConfig() IdentityConfig {
	ttl := utils.DurationFromEnv(
		[]string{"IDENTITY_LINK_TTL"},
		time.Duration(utils.GetEnv("IDENTITY_LINK_TTL_SECONDS", int(DefaultIdentityLinkTTL/time.Second)))*time.Second,
	)
	return IdentityConfig{TTL: ttl}
}
