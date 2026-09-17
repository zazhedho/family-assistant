package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/spf13/viper"
)

func TestGetAppConfReturnsDefaultWhenConfigMissing(t *testing.T) {
	viper.Reset()
	t.Setenv("CONSUL", "")
	t.Setenv("APP_CONFIG", t.TempDir())
	t.Setenv("APP_ENV", "test")

	if got := GetAppConf("MISSING_VALUE", "fallback", nil); got != "fallback" {
		t.Fatalf("expected fallback value, got %v", got)
	}
}

func TestGetAppConfLoadsLocalEnvFileAndExportsValues(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "app.env")
	if err := os.WriteFile(configPath, []byte("CONFIG_ID=file-config\nFEATURE_FLAG=enabled\nFEATURE_INT=42\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CONSUL", "")
	t.Setenv("APP_CONFIG", dir)
	t.Setenv("APP_ENV", "test")
	t.Setenv("CONFIG_ID", "old-config")

	if got := GetAppConf("FEATURE_FLAG", "fallback", nil); got != "enabled" {
		t.Fatalf("expected value from config file, got %v", got)
	}
	if got := os.Getenv("FEATURE_INT"); got != "42" {
		t.Fatalf("expected config value exported to env, got %q", got)
	}
}

func TestGetAppConfUsesCachedConsulConfig(t *testing.T) {
	viper.Reset()
	client, mock := redismock.NewClientMock()
	t.Setenv("CONSUL", "127.0.0.1:8500")
	t.Setenv("CONSUL_PATH", "starter")
	t.Setenv("APP_ENV", "test")
	t.Setenv("CACHE", "on")
	t.Setenv("CONFIG_ID", "old-config")

	mock.ExpectGet("cache:config:app").SetVal(`{"config_id":"cached-config","feature_flag":"cached"}`)
	if got := GetAppConf("FEATURE_FLAG", "fallback", client); got != "cached" {
		t.Fatalf("expected cached config value, got %v", got)
	}
	if got := os.Getenv("CONFIG_ID"); got != "cached-config" {
		t.Fatalf("expected cached config exported to env, got %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestLoadOTPConfigPrefersDurationStrings(t *testing.T) {
	t.Setenv("OTP_TTL_SECONDS", "300")
	t.Setenv("OTP_TTL", "7m")
	t.Setenv("OTP_COOLDOWN", "90s")
	t.Setenv("OTP_RATE_WINDOW", "10m")
	t.Setenv("OTP_MAX_ATTEMPTS", "3")
	t.Setenv("OTP_RATE_LIMIT", "9")
	t.Setenv("OTP_SECRET", " otp-secret ")

	got := LoadOTPConfig()
	if got.TTL != 7*time.Minute || got.Cooldown != 90*time.Second || got.RateWindow != 10*time.Minute {
		t.Fatalf("unexpected durations: %+v", got)
	}
	if got.MaxAttempts != 3 || got.RateLimit != 9 || got.Secret != "otp-secret" {
		t.Fatalf("unexpected scalar config: %+v", got)
	}
}

func TestLoadPasswordResetConfigUsesFallbackURL(t *testing.T) {
	t.Setenv("RESET_TTL", "20m")
	t.Setenv("RESET_COOLDOWN", "2m")
	t.Setenv("RESET_RATE_WINDOW", "30m")
	t.Setenv("RESET_RATE_LIMIT", "7")
	t.Setenv("RESET_SECRET", " reset-secret ")
	t.Setenv("RESET_URL_TEMPLATE", "")
	t.Setenv("RESET_URL", "https://example.com/reset?token={token}")

	got := LoadPasswordResetConfig()
	if got.TTL != 20*time.Minute || got.Cooldown != 2*time.Minute || got.RateWindow != 30*time.Minute {
		t.Fatalf("unexpected durations: %+v", got)
	}
	if got.RateLimit != 7 || got.Secret != "reset-secret" || got.URLTemplate != "https://example.com/reset?token={token}" {
		t.Fatalf("unexpected reset config: %+v", got)
	}
}
