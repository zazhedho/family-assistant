package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"family-assistant/utils"
)

const (
	defaultReminderSchedulerInterval = 30 * time.Second
	defaultReminderSchedulerBatch    = 50
	defaultReminderSchedulerLease    = 2 * time.Minute
)

type ReminderSchedulerConfig struct {
	Enabled           bool
	WhatsAppBridgeURL string
	Interval          time.Duration
	BatchSize         int
	Lease             time.Duration
}

func LoadReminderSchedulerConfig() ReminderSchedulerConfig {
	return ReminderSchedulerConfig{
		Enabled:           utils.GetEnv("REMINDER_SCHEDULER_ENABLED", false),
		WhatsAppBridgeURL: strings.TrimSpace(utils.GetEnv("REMINDER_WHATSAPP_BRIDGE_URL", "")),
		Interval:          utils.GetEnv("REMINDER_SCHEDULER_INTERVAL", defaultReminderSchedulerInterval),
		BatchSize:         utils.GetEnv("REMINDER_SCHEDULER_BATCH_SIZE", defaultReminderSchedulerBatch),
		Lease:             utils.GetEnv("REMINDER_SCHEDULER_LEASE", defaultReminderSchedulerLease),
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
	return nil
}
