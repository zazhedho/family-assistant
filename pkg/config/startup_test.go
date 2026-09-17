package config

import (
	"strings"
	"testing"
)

func TestValidateStartupConfigAcceptsRequiredOnly(t *testing.T) {
	clearStartupEnv(t)
	setRequiredStartupEnv(t)

	if err := ValidateStartupConfig("8080"); err != nil {
		t.Fatalf("expected valid startup config, got %v", err)
	}
}

func TestValidateStartupConfigAllowsDatabaseURLWithoutParts(t *testing.T) {
	clearStartupEnv(t)
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/app?sslmode=disable")

	if err := ValidateStartupConfig("8080"); err != nil {
		t.Fatalf("expected DATABASE_URL to satisfy db config, got %v", err)
	}
}

func TestValidateStartupConfigRequiresCoreConfig(t *testing.T) {
	clearStartupEnv(t)

	err := ValidateStartupConfig("")
	if err == nil {
		t.Fatal("expected startup config error")
	}

	message := err.Error()
	for _, want := range []string{
		"PORT is required",
		"jwt key is not configured",
		"DB_HOST is required when DATABASE_URL is empty",
		"DB_PORT is required when DATABASE_URL is empty",
		"DB_USERNAME is required when DATABASE_URL is empty",
		"DB_NAME is required when DATABASE_URL is empty",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected %q in error %q", want, message)
		}
	}
}

func TestValidateStartupConfigValidatesConfiguredSMTPOnly(t *testing.T) {
	clearStartupEnv(t)
	setRequiredStartupEnv(t)
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "bad-port")
	t.Setenv("SMTP_FROM", "not-an-email")

	err := ValidateStartupConfig("8080")
	if err == nil {
		t.Fatal("expected smtp config error")
	}

	message := err.Error()
	for _, want := range []string{
		"SMTP_PASS is required when SMTP is configured",
		"SMTP_PORT must be a number between 1 and 65535",
		"SMTP_FROM must be a valid email address",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected %q in error %q", want, message)
		}
	}
}

func TestValidateStartupConfigIgnoresOptionalLabelsWithoutConnectionConfig(t *testing.T) {
	clearStartupEnv(t)
	setRequiredStartupEnv(t)
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_USER", "apikey")
	t.Setenv("SMTP_SUBJECT", "Your OTP")
	t.Setenv("RESET_SUBJECT", "Reset Password")
	t.Setenv("STORAGE_PROVIDER", "local")

	if err := ValidateStartupConfig("8080"); err != nil {
		t.Fatalf("expected optional labels without connection config to be ignored, got %v", err)
	}
}

func TestValidateStartupConfigValidatesConfiguredRedisAndStorageOnly(t *testing.T) {
	clearStartupEnv(t)
	setRequiredStartupEnv(t)
	t.Setenv("REDIS_PORT", "70000")
	t.Setenv("REDIS_DB", "-1")
	t.Setenv("STORAGE_PROVIDER", "local")
	t.Setenv("STORAGE_ENDPOINT", "localhost:9000")

	err := ValidateStartupConfig("8080")
	if err == nil {
		t.Fatal("expected redis and storage config error")
	}

	message := err.Error()
	for _, want := range []string{
		"REDIS_PORT must be a number between 1 and 65535",
		"REDIS_DB must be a non-negative integer",
		"STORAGE_PROVIDER must be one of: minio, r2, cloudflare, cloudflare-r2",
		"STORAGE_ACCESS_KEY is required when storage is configured",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected %q in error %q", want, message)
		}
	}
}

func TestValidateStartupConfigRequiresStorageWhenMediaEnabled(t *testing.T) {
	clearStartupEnv(t)
	setRequiredStartupEnv(t)
	t.Setenv("MEDIA_ENABLED", "true")
	t.Setenv("MEDIA_MAX_FILE_SIZE_MB", "0")
	t.Setenv("STORAGE_PROVIDER", "r2")

	err := ValidateStartupConfig("8080")
	if err == nil {
		t.Fatal("expected media storage config error")
	}

	message := err.Error()
	for _, want := range []string{
		"STORAGE_ENDPOINT is required when storage is configured",
		"STORAGE_ACCESS_KEY is required when storage is configured",
		"STORAGE_BASE_URL is required when media is enabled",
		"MEDIA_MAX_FILE_SIZE_MB must be between 1 and 1024",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected %q in error %q", want, message)
		}
	}
}

func setRequiredStartupEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USERNAME", "starter")
	t.Setenv("DB_NAME", "starter")
}

func clearStartupEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"JWT_KEY",
		"DATABASE_URL",
		"DB_HOST",
		"DB_PORT",
		"DB_USERNAME",
		"DB_PASS",
		"DB_NAME",
		"DB_SSLMODE",
		"REDIS_URL",
		"REDIS_HOST",
		"REDIS_PORT",
		"REDIS_PASSWORD",
		"REDIS_DB",
		"SMTP_HOST",
		"SMTP_PORT",
		"SMTP_USER",
		"SMTP_PASS",
		"SMTP_FROM",
		"SMTP_SUBJECT",
		"RESET_SUBJECT",
		"MEDIA_ENABLED",
		"MEDIA_MAX_FILE_SIZE_MB",
		"MEDIA_ALLOWED_CONTENT_TYPES",
		"MEDIA_UPLOAD_RATE_LIMIT",
		"MEDIA_UPLOAD_RATE_WINDOW_SECONDS",
		"STORAGE_PROVIDER",
		"STORAGE_ENDPOINT",
		"STORAGE_ACCESS_KEY",
		"STORAGE_SECRET_KEY",
		"STORAGE_BUCKET_NAME",
		"STORAGE_BASE_URL",
		"R2_ACCOUNT_ID",
	} {
		t.Setenv(key, "")
	}
}
