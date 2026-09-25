package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadReminderSchedulerConfigDefaultsDisabled(t *testing.T) {
	for _, key := range []string{
		"REMINDER_SCHEDULER_ENABLED", "REMINDER_WHATSAPP_BRIDGE_URL",
		"REMINDER_SCHEDULER_INTERVAL", "REMINDER_SCHEDULER_BATCH_SIZE", "REMINDER_SCHEDULER_LEASE",
	} {
		t.Setenv(key, "")
	}

	got := LoadReminderSchedulerConfig()
	if got.Enabled || got.WhatsAppBridgeURL != "" || got.Interval != 30*time.Second || got.BatchSize != 50 || got.Lease != 2*time.Minute {
		t.Fatalf("config = %+v, want safe defaults", got)
	}
}

func TestValidateReminderSchedulerRequiresBridgeWhenEnabled(t *testing.T) {
	t.Setenv("REMINDER_SCHEDULER_ENABLED", "true")
	t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "")

	err := ValidateReminderSchedulerConfig()
	if err == nil || !strings.Contains(err.Error(), "REMINDER_WHATSAPP_BRIDGE_URL is required") {
		t.Fatalf("error = %v, want bridge URL validation", err)
	}
}

func TestLoadAndValidateReminderSchedulerConfig(t *testing.T) {
	t.Setenv("REMINDER_SCHEDULER_ENABLED", "true")
	t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://localhost:8765")
	t.Setenv("REMINDER_SCHEDULER_INTERVAL", "45s")
	t.Setenv("REMINDER_SCHEDULER_BATCH_SIZE", "12")
	t.Setenv("REMINDER_SCHEDULER_LEASE", "90s")

	got := LoadReminderSchedulerConfig()
	if err := ValidateReminderSchedulerConfig(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if got.Interval != 45*time.Second || got.BatchSize != 12 || got.Lease != 90*time.Second {
		t.Fatalf("config = %+v, want configured values", got)
	}
}
