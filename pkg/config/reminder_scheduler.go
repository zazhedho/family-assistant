package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"family-assistant/utils"
)

const (
	defaultReminderSchedulerInterval       = 2 * time.Minute
	defaultReminderSchedulerBatch          = 50
	defaultReminderSchedulerLease          = 2 * time.Minute
	defaultDatabaseFallbackInterval        = time.Minute
	defaultReminderIndexReconcileInterval  = time.Hour
	defaultReminderSchedulerIndexBatchSize = 1000
)

type ReminderSchedulerConfig struct {
	Enabled                  bool
	WhatsAppBridgeURL        string
	Interval                 time.Duration
	BatchSize                int
	Lease                    time.Duration
	DatabaseFallbackInterval time.Duration
	IndexReconcileInterval   time.Duration
	IndexBatchSize           int
}

func LoadReminderSchedulerConfig() ReminderSchedulerConfig {
	return ReminderSchedulerConfig{
		Enabled:                  utils.GetEnv("REMINDER_SCHEDULER_ENABLED", false),
		WhatsAppBridgeURL:        strings.TrimSpace(utils.GetEnv("REMINDER_WHATSAPP_BRIDGE_URL", "")),
		Interval:                 utils.GetEnv("REMINDER_SCHEDULER_INTERVAL", defaultReminderSchedulerInterval),
		BatchSize:                utils.GetEnv("REMINDER_SCHEDULER_BATCH_SIZE", defaultReminderSchedulerBatch),
		Lease:                    utils.GetEnv("REMINDER_SCHEDULER_LEASE", defaultReminderSchedulerLease),
		DatabaseFallbackInterval: utils.GetEnv("REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL", defaultDatabaseFallbackInterval),
		IndexReconcileInterval:   utils.GetEnv("REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL", defaultReminderIndexReconcileInterval),
		IndexBatchSize:           utils.GetEnv("REMINDER_SCHEDULER_INDEX_BATCH_SIZE", defaultReminderSchedulerIndexBatchSize),
	}
}

func ValidateReminderSchedulerConfig() error {
	conf := LoadReminderSchedulerConfig()
	if !conf.Enabled {
		return nil
	}
	if conf.WhatsAppBridgeURL == "" {
		return errors.New("REMINDER_WHATSAPP_BRIDGE_URL is required when REMINDER_SCHEDULER_ENABLED=true")
	}
	parsed, err := url.ParseRequestURI(conf.WhatsAppBridgeURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("REMINDER_WHATSAPP_BRIDGE_URL must be a valid HTTP URL")
	}
	if conf.Interval <= 0 {
		return errors.New("REMINDER_SCHEDULER_INTERVAL must be positive")
	}
	if conf.BatchSize <= 0 {
		return errors.New("REMINDER_SCHEDULER_BATCH_SIZE must be positive")
	}
	if conf.Lease <= 0 {
		return errors.New("REMINDER_SCHEDULER_LEASE must be positive")
	}
	if conf.DatabaseFallbackInterval <= 0 {
		return errors.New("REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL must be positive")
	}
	if conf.IndexReconcileInterval <= 0 {
		return errors.New("REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL must be positive")
	}
	if conf.IndexBatchSize <= 0 {
		return errors.New("REMINDER_SCHEDULER_INDEX_BATCH_SIZE must be positive")
	}
	return nil
}
