package config

import (
	"testing"
	"time"
)

func TestLoadFinanceRecurringConfigDefaultsEnabledEveryMinute(t *testing.T) {
	t.Setenv("FINANCE_RECURRING_ENABLED", "")
	t.Setenv("FINANCE_RECURRING_INTERVAL", "")
	got := LoadFinanceRecurringConfig()
	if !got.Enabled || got.Interval != time.Minute {
		t.Fatalf("config = %+v, want enabled every 60s", got)
	}
}

func TestLoadFinanceRecurringConfigFromEnv(t *testing.T) {
	t.Setenv("FINANCE_RECURRING_ENABLED", "false")
	t.Setenv("FINANCE_RECURRING_INTERVAL", "90s")
	got := LoadFinanceRecurringConfig()
	if got.Enabled || got.Interval != 90*time.Second {
		t.Fatalf("config = %+v", got)
	}
}

func TestLoadFinanceRecurringConfigRejectsNonPositiveInterval(t *testing.T) {
	t.Setenv("FINANCE_RECURRING_INTERVAL", "-5s")
	if got := LoadFinanceRecurringConfig(); got.Interval != time.Minute {
		t.Fatalf("interval = %v, want default", got.Interval)
	}
}
