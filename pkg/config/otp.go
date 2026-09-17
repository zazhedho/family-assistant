package config

import (
	"time"

	"starter-kit/utils"
)

type OTPConfig struct {
	TTL         time.Duration
	MaxAttempts int
	RateLimit   int
	RateWindow  time.Duration
	Cooldown    time.Duration
	Secret      string
}

func LoadOTPConfig() OTPConfig {
	ttl := utils.DurationFromEnv([]string{"OTP_TTL"}, time.Duration(utils.GetEnv("OTP_TTL_SECONDS", 300))*time.Second)
	cooldown := utils.DurationFromEnv([]string{"OTP_COOLDOWN"}, time.Duration(utils.GetEnv("OTP_COOLDOWN_SECONDS", 60))*time.Second)
	rateWindow := utils.DurationFromEnv([]string{"OTP_RATE_WINDOW"}, time.Duration(utils.GetEnv("OTP_RATE_WINDOW_SECONDS", int(ttl.Seconds())))*time.Second)

	return OTPConfig{
		TTL:         ttl,
		MaxAttempts: utils.GetEnv("OTP_MAX_ATTEMPTS", 5),
		RateLimit:   utils.GetEnv("OTP_RATE_LIMIT", 5),
		RateWindow:  rateWindow,
		Cooldown:    cooldown,
		Secret:      utils.GetEnv("OTP_SECRET", "otp-secret"),
	}
}
