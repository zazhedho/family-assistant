package config

import (
	"testing"
	"time"
)

func TestLoadInvitationConfigDefaultsTo72Hours(t *testing.T) {
	t.Setenv("SPACE_INVITATION_TTL", "")
	t.Setenv("SPACE_INVITATION_TTL_SECONDS", "")

	got := LoadInvitationConfig()
	if got.TTL != 72*time.Hour {
		t.Fatalf("TTL = %s, want 72h", got.TTL)
	}
}

func TestLoadInvitationConfigUsesConfiguredDuration(t *testing.T) {
	t.Setenv("SPACE_INVITATION_TTL", "2h")

	got := LoadInvitationConfig()
	if got.TTL != 2*time.Hour {
		t.Fatalf("TTL = %s, want 2h", got.TTL)
	}
}
