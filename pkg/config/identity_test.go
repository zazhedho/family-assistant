package config

import (
	"testing"
	"time"
)

func TestLoadIdentityConfigDefaultsToTenMinutes(t *testing.T) {
	t.Setenv("IDENTITY_LINK_TTL", "")
	t.Setenv("IDENTITY_LINK_TTL_SECONDS", "")

	got := LoadIdentityConfig()
	if got.TTL != 10*time.Minute {
		t.Fatalf("TTL = %s, want 10m", got.TTL)
	}
}

func TestLoadIdentityConfigUsesConfiguredDuration(t *testing.T) {
	t.Setenv("IDENTITY_LINK_TTL", "2m")

	got := LoadIdentityConfig()
	if got.TTL != 2*time.Minute {
		t.Fatalf("TTL = %s, want 2m", got.TTL)
	}
}
