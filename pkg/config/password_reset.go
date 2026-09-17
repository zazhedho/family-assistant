package config

import (
	"time"

	"starter-kit/utils"
)

type PasswordResetConfig struct {
	TTL         time.Duration
	Cooldown    time.Duration
	RateWindow  time.Duration
	RateLimit   int
	Secret      string
	URLTemplate string
}

func LoadPasswordResetConfig() PasswordResetConfig {
	ttl := utils.DurationFromEnv([]string{"RESET_TTL"}, time.Duration(utils.GetEnv("RESET_TTL_SECONDS", 900))*time.Second)
	cooldown := utils.DurationFromEnv([]string{"RESET_COOLDOWN"}, time.Duration(utils.GetEnv("RESET_COOLDOWN_SECONDS", 60))*time.Second)
	rateWindow := utils.DurationFromEnv([]string{"RESET_RATE_WINDOW"}, time.Duration(utils.GetEnv("RESET_RATE_WINDOW_SECONDS", int(ttl.Seconds())))*time.Second)

	urlTemplate := utils.GetEnv("RESET_URL_TEMPLATE", "")
	if urlTemplate == "" {
		urlTemplate = utils.GetEnv("RESET_URL", "")
	}

	return PasswordResetConfig{
		TTL:         ttl,
		Cooldown:    cooldown,
		RateWindow:  rateWindow,
		RateLimit:   utils.GetEnv("RESET_RATE_LIMIT", 5),
		Secret:      utils.GetEnv("RESET_SECRET", "reset-secret"),
		URLTemplate: urlTemplate,
	}
}
