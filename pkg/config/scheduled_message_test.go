package config

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadScheduledMessageConfigDefaultsDisabledWithoutHermesCredentials(t *testing.T) {
	clearScheduledMessageEnv(t)
	got := LoadScheduledMessageConfig()
	if got.Enabled || got.GeneratorURL != "" || got.GeneratorKey != "" || got.GeneratorModel != "scheduled-messages" {
		t.Fatalf("config = %+v, want disabled and unconfigured defaults", got)
	}
	if got.GeneratorTimeout != 30*time.Second || got.MaxResponseBytes != 64*1024 || got.MaxOutputBytes != 2000 ||
		got.GenerationMaxRetries != 3 || got.DeliveryMaxRetries != 3 || got.Lease != 2*time.Minute ||
		got.BatchSize != 10 || got.Concurrency != 10 || got.MaxTextBytes != 2000 || got.MaxListRows != 100 {
		t.Fatalf("config = %+v, want bounded scheduled-message defaults", got)
	}
	if err := ValidateScheduledMessageConfig(); err != nil {
		t.Fatalf("disabled scheduled messages should not require generator credentials: %v", err)
	}
}

func TestValidateScheduledMessageConfigAcceptsPrivateHermesEndpointForms(t *testing.T) {
	for _, endpoint := range []string{
		"http://127.0.0.1:8642/v1/chat/completions",
		"http://127.0.0.1:8642/p/scheduled-messages/v1/chat/completions",
	} {
		t.Run(endpoint, func(t *testing.T) {
			setScheduledMessageEnv(t)
			t.Setenv("SCHEDULED_MESSAGES_GENERATOR_URL", endpoint)
			t.Setenv("SCHEDULED_MESSAGES_GENERATOR_KEY", "unit-test-profile-key")
			t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8765")
			if err := ValidateScheduledMessageConfig(); err != nil {
				t.Fatalf("valid private endpoint rejected: %v", err)
			}
		})
	}
}

func TestIsValidHermesChatCompletionsURL(t *testing.T) {
	for _, test := range []struct {
		url   string
		valid bool
	}{
		{url: "http://127.0.0.1:8642/v1/chat/completions", valid: true},
		{url: "http://127.0.0.1:8642/p/scheduled-messages/v1/chat/completions", valid: true},
		{url: "http://127.0.0.1:8642/v1", valid: false},
		{url: "http://user@127.0.0.1:8642/v1/chat/completions", valid: false},
		{url: "http://127.0.0.1:8642/v1/chat/completions?key=hidden", valid: false},
	} {
		if got := IsValidHermesChatCompletionsURL(test.url); got != test.valid {
			t.Errorf("IsValidHermesChatCompletionsURL(%q) = %t, want %t", test.url, got, test.valid)
		}
	}
}

func TestValidateScheduledMessageConfigRejectsPartialGeneratorAndUnsafeEndpoint(t *testing.T) {
	for _, test := range []struct {
		name     string
		url      string
		key      string
		wantText string
	}{
		{name: "missing key", url: "http://127.0.0.1:8642/v1/chat/completions", wantText: "SCHEDULED_MESSAGES_GENERATOR_KEY"},
		{name: "not a full endpoint", url: "http://127.0.0.1:8642/v1", key: "unit-test-profile-key", wantText: "SCHEDULED_MESSAGES_GENERATOR_URL"},
		{name: "userinfo", url: "http://user@127.0.0.1:8642/v1/chat/completions", key: "unit-test-profile-key", wantText: "SCHEDULED_MESSAGES_GENERATOR_URL"},
		{name: "query string", url: "http://127.0.0.1:8642/v1/chat/completions?key=hidden", key: "unit-test-profile-key", wantText: "SCHEDULED_MESSAGES_GENERATOR_URL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setScheduledMessageEnv(t)
			t.Setenv("SCHEDULED_MESSAGES_GENERATOR_URL", test.url)
			t.Setenv("SCHEDULED_MESSAGES_GENERATOR_KEY", test.key)
			t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8765")
			err := ValidateScheduledMessageConfig()
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("validation error = %v, want %s", err, test.wantText)
			}
		})
	}
}

func TestValidateScheduledMessageConfigBoundsLeaseBatchConcurrencyAndListRows(t *testing.T) {
	for _, test := range []struct {
		name     string
		key      string
		value    string
		wantText string
	}{
		{name: "lease shorter than bounded external work", key: "SCHEDULED_MESSAGES_LEASE", value: "30s", wantText: "SCHEDULED_MESSAGES_LEASE"},
		{name: "batch exceeds concurrency", key: "SCHEDULED_MESSAGES_BATCH_SIZE", value: "11", wantText: "SCHEDULED_MESSAGES_BATCH_SIZE"},
		{name: "list cap exceeds hard limit", key: "SCHEDULED_MESSAGES_MAX_LIST_ROWS", value: "101", wantText: "SCHEDULED_MESSAGES_MAX_LIST_ROWS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setScheduledMessageEnv(t)
			t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8765")
			if test.key == "SCHEDULED_MESSAGES_BATCH_SIZE" {
				t.Setenv("SCHEDULED_MESSAGES_CONCURRENCY", "10")
			}
			t.Setenv(test.key, test.value)
			err := ValidateScheduledMessageConfig()
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("validation error = %v, want %s", err, test.wantText)
			}
		})
	}
}

func TestValidateScheduledMessageConfigCapsRetriesAtThree(t *testing.T) {
	for _, key := range []string{"SCHEDULED_MESSAGES_GENERATION_MAX_RETRIES", "SCHEDULED_MESSAGES_DELIVERY_MAX_RETRIES"} {
		t.Run(key, func(t *testing.T) {
			setScheduledMessageEnv(t)
			t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8765")
			t.Setenv(key, "4")
			if err := ValidateScheduledMessageConfig(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("validation error = %v, want %s retry cap", err, key)
			}
		})
	}
}

func TestScheduledMessageConfigValueValidationRejectsUnsafeValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*ScheduledMessageConfig)
		want   string
	}{
		{name: "too many generation retries", change: func(cfg *ScheduledMessageConfig) { cfg.GenerationMaxRetries = 4 }, want: "SCHEDULED_MESSAGES_GENERATION_MAX_RETRIES"},
		{name: "too many delivery retries", change: func(cfg *ScheduledMessageConfig) { cfg.DeliveryMaxRetries = 4 }, want: "SCHEDULED_MESSAGES_DELIVERY_MAX_RETRIES"},
		{name: "batch exceeds concurrency", change: func(cfg *ScheduledMessageConfig) { cfg.BatchSize = cfg.Concurrency + 1 }, want: "SCHEDULED_MESSAGES_BATCH_SIZE"},
		{name: "lease shorter than operation bound", change: func(cfg *ScheduledMessageConfig) { cfg.Lease = 30 * time.Second }, want: "SCHEDULED_MESSAGES_LEASE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setScheduledMessageEnv(t)
			t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8765")
			cfg := LoadScheduledMessageConfig()
			test.change(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestValidateScheduledMessageConfigRejectsOverflowingOperationLease(t *testing.T) {
	setScheduledMessageEnv(t)
	t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8765")
	maxDuration := "2562047h47m16.854775807s"
	t.Setenv("SCHEDULED_MESSAGES_GENERATOR_TIMEOUT", maxDuration)
	t.Setenv("SCHEDULED_MESSAGES_LEASE", maxDuration)
	if err := ValidateScheduledMessageConfig(); err == nil || !strings.Contains(err.Error(), "SCHEDULED_MESSAGES_LEASE") {
		t.Fatalf("validation error = %v, want lease error when timeout sum overflows", err)
	}
}

func TestValidateScheduledMessageConfigRejectsUnrepresentableResponseLimit(t *testing.T) {
	setScheduledMessageEnv(t)
	t.Setenv("REMINDER_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8765")
	t.Setenv("SCHEDULED_MESSAGES_GENERATOR_MAX_RESPONSE_BYTES", strconv.Itoa(int(^uint(0)>>1)))
	if err := ValidateScheduledMessageConfig(); err == nil || !strings.Contains(err.Error(), "SCHEDULED_MESSAGES_GENERATOR_MAX_RESPONSE_BYTES") {
		t.Fatalf("validation error = %v, want response-limit error at MaxInt", err)
	}
}

func TestLoadScheduledMessageConfigReadsRetryAndTextLimits(t *testing.T) {
	clearScheduledMessageEnv(t)
	t.Setenv("SCHEDULED_MESSAGES_GENERATION_MAX_RETRIES", "4")
	t.Setenv("SCHEDULED_MESSAGES_DELIVERY_MAX_RETRIES", "2")
	t.Setenv("SCHEDULED_MESSAGES_MAX_TEXT_BYTES", "4096")
	t.Setenv("SCHEDULED_MESSAGES_MAX_LIST_ROWS", "40")
	got := LoadScheduledMessageConfig()
	if got.GenerationMaxRetries != 4 || got.DeliveryMaxRetries != 2 || got.MaxTextBytes != 4096 || got.MaxListRows != 40 {
		t.Fatalf("config = %+v, want configured retries and limits", got)
	}
}

func TestValidateStartupConfigIncludesScheduledMessageValidation(t *testing.T) {
	clearStartupEnv(t)
	clearScheduledMessageEnv(t)
	setRequiredStartupEnv(t)
	t.Setenv("SCHEDULED_MESSAGES_ENABLED", "true")
	err := ValidateStartupConfig("8080")
	if err == nil || !strings.Contains(err.Error(), "REMINDER_WHATSAPP_BRIDGE_URL is required") {
		t.Fatalf("startup validation error = %v, want shared bridge URL requirement", err)
	}
}

func setScheduledMessageEnv(t *testing.T) {
	t.Helper()
	clearScheduledMessageEnv(t)
	t.Setenv("SCHEDULED_MESSAGES_ENABLED", "true")
}

func clearScheduledMessageEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SCHEDULED_MESSAGES_ENABLED", "SCHEDULED_MESSAGES_GENERATOR_URL", "SCHEDULED_MESSAGES_GENERATOR_KEY",
		"SCHEDULED_MESSAGES_GENERATOR_MODEL", "SCHEDULED_MESSAGES_GENERATOR_TIMEOUT", "SCHEDULED_MESSAGES_GENERATOR_MAX_RESPONSE_BYTES",
		"SCHEDULED_MESSAGES_MAX_OUTPUT_BYTES", "SCHEDULED_MESSAGES_GENERATION_MAX_RETRIES", "SCHEDULED_MESSAGES_GENERATION_RETRY_BACKOFF",
		"SCHEDULED_MESSAGES_DELIVERY_MAX_RETRIES", "SCHEDULED_MESSAGES_DELIVERY_RETRY_BACKOFF", "SCHEDULED_MESSAGES_LEASE",
		"SCHEDULED_MESSAGES_BATCH_SIZE", "SCHEDULED_MESSAGES_CONCURRENCY", "SCHEDULED_MESSAGES_MAX_TEXT_BYTES", "SCHEDULED_MESSAGES_MAX_LIST_ROWS",
		"REMINDER_WHATSAPP_BRIDGE_URL", "REMINDER_SCHEDULER_ENABLED", "REMINDER_SCHEDULER_INTERVAL",
		"REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL", "REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL", "REMINDER_SCHEDULER_INDEX_BATCH_SIZE",
	} {
		t.Setenv(key, "")
	}
}
