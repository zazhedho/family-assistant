package config

import (
	"errors"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"

	"starter-kit/utils"
)

func ValidateStartupConfig(port string) error {
	var problems []string

	problems = append(problems, validateRequiredPort(port)...)
	if err := utils.ValidateJWTKeyConfigured(); err != nil {
		problems = append(problems, err.Error())
	}
	problems = append(problems, validateDatabaseConfig()...)
	problems = append(problems, validateOptionalRedisConfig()...)
	problems = append(problems, validateOptionalSMTPConfig()...)
	problems = append(problems, validateOptionalStorageConfig()...)

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validateRequiredPort(port string) []string {
	port = strings.TrimSpace(port)
	if port == "" {
		return []string{"PORT is required"}
	}
	if !validPort(port) {
		return []string{"PORT must be a number between 1 and 65535"}
	}
	return nil
}

func validateDatabaseConfig() []string {
	if utils.GetEnv("DATABASE_URL", "") != "" {
		return nil
	}

	var problems []string
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_USERNAME", "DB_NAME"} {
		if utils.GetEnv(key, "") == "" {
			problems = append(problems, key+" is required when DATABASE_URL is empty")
		}
	}
	if port := utils.GetEnv("DB_PORT", ""); port != "" && !validPort(port) {
		problems = append(problems, "DB_PORT must be a number between 1 and 65535")
	}
	return problems
}

func validateOptionalRedisConfig() []string {
	if !hasAnyEnv("REDIS_URL", "REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB") {
		return nil
	}

	var problems []string
	if rawURL := utils.GetEnv("REDIS_URL", ""); rawURL != "" {
		if _, err := redis.ParseURL(rawURL); err != nil {
			problems = append(problems, "REDIS_URL is invalid")
		}
	}
	if port := utils.GetEnv("REDIS_PORT", ""); port != "" && !validPort(port) {
		problems = append(problems, "REDIS_PORT must be a number between 1 and 65535")
	}
	if db := utils.GetEnv("REDIS_DB", ""); db != "" {
		if parsed, err := strconv.Atoi(db); err != nil || parsed < 0 {
			problems = append(problems, "REDIS_DB must be a non-negative integer")
		}
	}
	return problems
}

func validateOptionalSMTPConfig() []string {
	if !hasAnyEnv("SMTP_HOST", "SMTP_PASS", "SMTP_FROM") {
		return nil
	}

	var problems []string
	for _, key := range []string{"SMTP_HOST", "SMTP_PASS", "SMTP_FROM"} {
		if utils.GetEnv(key, "") == "" {
			problems = append(problems, key+" is required when SMTP is configured")
		}
	}
	if port := utils.GetEnv("SMTP_PORT", ""); port != "" && !validPort(port) {
		problems = append(problems, "SMTP_PORT must be a number between 1 and 65535")
	}
	if from := utils.GetEnv("SMTP_FROM", ""); from != "" {
		if _, err := mail.ParseAddress(from); err != nil {
			problems = append(problems, "SMTP_FROM must be a valid email address")
		}
	}
	return problems
}

func validateOptionalStorageConfig() []string {
	mediaEnabled := utils.GetEnv("MEDIA_ENABLED", false)
	if !mediaEnabled && !hasAnyEnv("STORAGE_ENDPOINT", "STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY", "STORAGE_BUCKET_NAME", "STORAGE_BASE_URL", "R2_ACCOUNT_ID") {
		return nil
	}

	var problems []string
	provider := utils.NormalizeKey(utils.GetEnv("STORAGE_PROVIDER", ""))
	if provider == "" {
		provider = "minio"
	}
	switch provider {
	case "minio", "r2", "cloudflare", "cloudflare-r2":
	default:
		problems = append(problems, "STORAGE_PROVIDER must be one of: minio, r2, cloudflare, cloudflare-r2")
	}

	for _, key := range []string{"STORAGE_ENDPOINT", "STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY", "STORAGE_BUCKET_NAME"} {
		if utils.GetEnv(key, "") == "" {
			problems = append(problems, key+" is required when storage is configured")
		}
	}
	if mediaEnabled && utils.GetEnv("STORAGE_BASE_URL", "") == "" {
		problems = append(problems, "STORAGE_BASE_URL is required when media is enabled")
	}
	if baseURL := utils.GetEnv("STORAGE_BASE_URL", ""); baseURL != "" {
		if _, err := url.ParseRequestURI(baseURL); err != nil {
			problems = append(problems, "STORAGE_BASE_URL must be a valid URL")
		}
	}
	if maxSize := utils.GetEnv("MEDIA_MAX_FILE_SIZE_MB", 5); maxSize <= 0 || maxSize > 1024 {
		problems = append(problems, "MEDIA_MAX_FILE_SIZE_MB must be between 1 and 1024")
	}
	return problems
}

func validPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}

func hasAnyEnv(keys ...string) bool {
	for _, key := range keys {
		if utils.GetEnv(key, "") != "" {
			return true
		}
	}
	return false
}
