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
		"REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL", "REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL",
		"REMINDER_SCHEDULER_INDEX_BATCH_SIZE",
	} {
		t.Setenv(key, "")
	}

	got := LoadReminderSchedulerConfig()
	if got.Enabled || got.WhatsAppBridgeURL != "" || got.Interval != 2*time.Minute || got.BatchSize != 50 || got.Lease != 2*time.Minute ||
		got.DatabaseFallbackInterval != time.Minute || got.IndexReconcileInterval != time.Hour || got.IndexBatchSize != 1000 {
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
	t.Setenv("REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL", "3m")
	t.Setenv("REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL", "6h")
	t.Setenv("REMINDER_SCHEDULER_INDEX_BATCH_SIZE", "500")

	got := LoadReminderSchedulerConfig()
	if err := ValidateReminderSchedulerConfig(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if got.Interval != 45*time.Second || got.BatchSize != 12 || got.Lease != 90*time.Second ||
		got.DatabaseFallbackInterval != 3*time.Minute || got.IndexReconcileInterval != 6*time.Hour || got.IndexBatchSize != 500 {
		t.Fatalf("config = %+v, want configured values", got)
	}
}

func TestValidateReminderSchedulerRejectsNonPositiveTuningSettings(t *testing.T) {
	t.Setenv("REMINDER_SCHEDULER_ENABLED", "true")
	t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://localhost:8765")
	t.Setenv("REMINDER_SCHEDULER_INTERVAL", "1m")
	t.Setenv("REMINDER_SCHEDULER_BATCH_SIZE", "1")
	t.Setenv("REMINDER_SCHEDULER_LEASE", "1m")
	t.Setenv("REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL", "1m")
	t.Setenv("REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL", "1h")
	t.Setenv("REMINDER_SCHEDULER_INDEX_BATCH_SIZE", "1")

	for _, test := range []struct {
		name      string
		envKey    string
		value     string
		wantError string
	}{
		{name: "database fallback interval", envKey: "REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL", value: "0s"},
		{name: "index reconcile interval", envKey: "REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL", value: "-1s"},
		{name: "index batch size", envKey: "REMINDER_SCHEDULER_INDEX_BATCH_SIZE", value: "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.envKey, test.value)
			err := ValidateReminderSchedulerConfig()
			if err == nil || !strings.Contains(err.Error(), test.envKey) {
				t.Fatalf("validation error = %v, want %s", err, test.envKey)
			}
		})
	}
}
