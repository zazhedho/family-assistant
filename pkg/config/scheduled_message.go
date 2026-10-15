package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"family-assistant/utils"
)

const (
	defaultScheduledMessageGeneratorTimeout = 30 * time.Second
	defaultScheduledMessageMaxResponseBytes = 64 * 1024
	defaultScheduledMessageMaxOutputBytes   = 2000
	defaultScheduledMessageRetryBackoff     = time.Minute
	defaultScheduledMessageLease            = 2 * time.Minute
	defaultScheduledMessageBatchSize        = 10
	defaultScheduledMessageConcurrency      = 10
	defaultScheduledMessageMaxTextBytes     = 2000
	defaultScheduledMessageMaxListRows      = 100
	maxScheduledMessageListRows             = 100
	scheduledMessageProcessingMargin        = 15 * time.Second
	// Keep aligned with infrastructure/notification.whatsappBridgeTimeout.
	scheduledMessageBridgeTimeout = 10 * time.Second
)

type ScheduledMessageConfig struct {
	Enabled                bool
	Scheduler              ReminderSchedulerConfig
	GeneratorURL           string
	GeneratorKey           string
	GeneratorModel         string
	GeneratorTimeout       time.Duration
	MaxResponseBytes       int
	MaxOutputBytes         int
	GenerationMaxRetries   int
	GenerationRetryBackoff time.Duration
	DeliveryMaxRetries     int
	DeliveryRetryBackoff   time.Duration
	Lease                  time.Duration
	BatchSize              int
	Concurrency            int
	MaxTextBytes           int
	MaxListRows            int
}

func LoadScheduledMessageConfig() ScheduledMessageConfig {
	model := strings.TrimSpace(utils.GetEnv("SCHEDULED_MESSAGES_GENERATOR_MODEL", "scheduled-messages"))
	if model == "" {
		model = "scheduled-messages"
	}
	return ScheduledMessageConfig{
		Enabled:                utils.GetEnv("SCHEDULED_MESSAGES_ENABLED", false),
		Scheduler:              LoadReminderSchedulerConfig(),
		GeneratorURL:           strings.TrimSpace(utils.GetEnv("SCHEDULED_MESSAGES_GENERATOR_URL", "")),
		GeneratorKey:           strings.TrimSpace(utils.GetEnv("SCHEDULED_MESSAGES_GENERATOR_KEY", "")),
		GeneratorModel:         model,
		GeneratorTimeout:       utils.GetEnv("SCHEDULED_MESSAGES_GENERATOR_TIMEOUT", defaultScheduledMessageGeneratorTimeout),
		MaxResponseBytes:       utils.GetEnv("SCHEDULED_MESSAGES_GENERATOR_MAX_RESPONSE_BYTES", defaultScheduledMessageMaxResponseBytes),
		MaxOutputBytes:         utils.GetEnv("SCHEDULED_MESSAGES_MAX_OUTPUT_BYTES", defaultScheduledMessageMaxOutputBytes),
		GenerationMaxRetries:   utils.GetEnv("SCHEDULED_MESSAGES_GENERATION_MAX_RETRIES", 3),
		GenerationRetryBackoff: utils.GetEnv("SCHEDULED_MESSAGES_GENERATION_RETRY_BACKOFF", defaultScheduledMessageRetryBackoff),
		DeliveryMaxRetries:     utils.GetEnv("SCHEDULED_MESSAGES_DELIVERY_MAX_RETRIES", 3),
		DeliveryRetryBackoff:   utils.GetEnv("SCHEDULED_MESSAGES_DELIVERY_RETRY_BACKOFF", defaultScheduledMessageRetryBackoff),
		Lease:                  utils.GetEnv("SCHEDULED_MESSAGES_LEASE", defaultScheduledMessageLease),
		BatchSize:              utils.GetEnv("SCHEDULED_MESSAGES_BATCH_SIZE", defaultScheduledMessageBatchSize),
		Concurrency:            utils.GetEnv("SCHEDULED_MESSAGES_CONCURRENCY", defaultScheduledMessageConcurrency),
		MaxTextBytes:           utils.GetEnv("SCHEDULED_MESSAGES_MAX_TEXT_BYTES", defaultScheduledMessageMaxTextBytes),
		MaxListRows:            utils.GetEnv("SCHEDULED_MESSAGES_MAX_LIST_ROWS", defaultScheduledMessageMaxListRows),
	}
}

func ValidateScheduledMessageConfig() error {
	return LoadScheduledMessageConfig().Validate()
}

func (cfg ScheduledMessageConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Scheduler.WhatsAppBridgeURL == "" {
		return errors.New("REMINDER_WHATSAPP_BRIDGE_URL is required when SCHEDULED_MESSAGES_ENABLED=true")
	}
	if !validHTTPURL(cfg.Scheduler.WhatsAppBridgeURL) {
		return errors.New("REMINDER_WHATSAPP_BRIDGE_URL must be a valid HTTP URL when SCHEDULED_MESSAGES_ENABLED=true")
	}
	if cfg.Scheduler.Interval <= 0 || cfg.Scheduler.DatabaseFallbackInterval <= 0 || cfg.Scheduler.IndexReconcileInterval <= 0 || cfg.Scheduler.IndexBatchSize <= 0 {
		return errors.New("shared REMINDER_SCHEDULER interval, fallback, and reconcile settings must be positive")
	}
	if (cfg.GeneratorURL == "") != (cfg.GeneratorKey == "") {
		return errors.New("SCHEDULED_MESSAGES_GENERATOR_URL and SCHEDULED_MESSAGES_GENERATOR_KEY must be configured together")
	}
	if cfg.GeneratorURL != "" && !IsValidHermesChatCompletionsURL(cfg.GeneratorURL) {
		return errors.New("SCHEDULED_MESSAGES_GENERATOR_URL must be a full private Hermes chat completions URL")
	}
	if cfg.GeneratorTimeout <= 0 {
		return errors.New("SCHEDULED_MESSAGES_GENERATOR_TIMEOUT must be positive")
	}
	if cfg.MaxResponseBytes <= 0 || cfg.MaxResponseBytes == int(^uint(0)>>1) {
		return errors.New("SCHEDULED_MESSAGES_GENERATOR_MAX_RESPONSE_BYTES must be positive and representable for bounded reads")
	}
	if cfg.MaxOutputBytes <= 0 {
		return errors.New("SCHEDULED_MESSAGES_MAX_OUTPUT_BYTES must be positive")
	}
	if cfg.GenerationMaxRetries < 0 || cfg.GenerationMaxRetries > 3 {
		return errors.New("SCHEDULED_MESSAGES_GENERATION_MAX_RETRIES must be between 0 and 3")
	}
	if cfg.DeliveryMaxRetries < 0 || cfg.DeliveryMaxRetries > 3 {
		return errors.New("SCHEDULED_MESSAGES_DELIVERY_MAX_RETRIES must be between 0 and 3")
	}
	if cfg.GenerationRetryBackoff < 0 || cfg.DeliveryRetryBackoff < 0 {
		return errors.New("scheduled message retry backoffs must be non-negative")
	}
	operationMargin := scheduledMessageBridgeTimeout + scheduledMessageProcessingMargin
	if cfg.Lease <= operationMargin || cfg.GeneratorTimeout >= cfg.Lease-operationMargin {
		return errors.New("SCHEDULED_MESSAGES_LEASE must exceed the generator and WhatsApp operation timeouts plus processing margin")
	}
	if cfg.BatchSize <= 0 || cfg.Concurrency <= 0 || cfg.BatchSize > cfg.Concurrency {
		return errors.New("SCHEDULED_MESSAGES_BATCH_SIZE must be positive and no greater than SCHEDULED_MESSAGES_CONCURRENCY")
	}
	if cfg.MaxTextBytes <= 0 {
		return errors.New("SCHEDULED_MESSAGES_MAX_TEXT_BYTES must be positive")
	}
	if cfg.MaxListRows <= 0 || cfg.MaxListRows > maxScheduledMessageListRows {
		return errors.New("SCHEDULED_MESSAGES_MAX_LIST_ROWS must be between 1 and 100")
	}
	return nil
}

func validHTTPURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func IsValidHermesChatCompletionsURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	const endpoint = "/v1/chat/completions"
	path := parsed.EscapedPath()
	if path == endpoint {
		return true
	}
	profilePath := strings.TrimSuffix(path, endpoint)
	if profilePath == path || !strings.HasPrefix(profilePath, "/p/") {
		return false
	}
	profile := strings.TrimPrefix(profilePath, "/p/")
	return profile != "" && !strings.Contains(profile, "/")
}
