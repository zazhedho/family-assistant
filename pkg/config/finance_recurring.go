package config

import (
	"time"

	"family-assistant/utils"
)

const defaultFinanceRecurringInterval = time.Minute

// FinanceRecurringConfig controls the scheduler that posts recurring finance
// transactions. It is on by default; FINANCE_RECURRING_ENABLED=false turns
// it off and FINANCE_RECURRING_INTERVAL sets the tick ("60s", "2m" or seconds).
type FinanceRecurringConfig struct {
	Enabled  bool
	Interval time.Duration
}

func LoadFinanceRecurringConfig() FinanceRecurringConfig {
	cfg := FinanceRecurringConfig{
		Enabled:  utils.GetEnv("FINANCE_RECURRING_ENABLED", true),
		Interval: utils.GetEnv("FINANCE_RECURRING_INTERVAL", defaultFinanceRecurringInterval),
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultFinanceRecurringInterval
	}
	return cfg
}
