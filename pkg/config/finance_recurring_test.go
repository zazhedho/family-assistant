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

func TestLoadFinanceRecurringConfigRejectsIntervalBelowOneSecond(t *testing.T) {
	for _, v := range []string{"-5s", "0", "500ms"} {
		t.Setenv("FINANCE_RECURRING_INTERVAL", v)
		if got := LoadFinanceRecurringConfig(); got.Interval != time.Minute {
			t.Fatalf("%s: interval = %v, want default", v, got.Interval)
		}
	}
	t.Setenv("FINANCE_RECURRING_INTERVAL", "1s")
	if got := LoadFinanceRecurringConfig(); got.Interval != time.Second {
		t.Fatalf("1s: interval = %v", got.Interval)
	}
}
