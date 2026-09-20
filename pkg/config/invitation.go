package config

import (
	"time"

	"family-assistant/utils"
)

const DefaultInvitationTTL = 72 * time.Hour

type InvitationConfig struct {
	TTL time.Duration
}

func LoadInvitationConfig() InvitationConfig {
	ttl := utils.DurationFromEnv(
		[]string{"SPACE_INVITATION_TTL"},
		time.Duration(utils.GetEnv("SPACE_INVITATION_TTL_SECONDS", int(DefaultInvitationTTL/time.Second)))*time.Second,
	)
	return InvitationConfig{TTL: ttl}
}
