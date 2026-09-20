package config

import (
	"testing"
	"time"
)

func TestAgeOnUsesCalendarBirthday(t *testing.T) {
	today := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	if got := AgeOn(time.Date(2008, 2, 29, 0, 0, 0, 0, time.UTC), today); got != 18 {
		t.Fatalf("age=%d", got)
	}
	if got := AgeOn(time.Date(2008, 3, 1, 0, 0, 0, 0, time.UTC), today); got != 17 {
		t.Fatalf("age=%d", got)
	}
}

func TestLoadMinimumIndependentAccountAgeDefaultsAndReadsEnvironment(t *testing.T) {
	t.Setenv("MIN_INDEPENDENT_ACCOUNT_AGE", "")
	if got := LoadMinimumIndependentAccountAge(); got != 18 {
		t.Fatalf("default minimum age=%d", got)
	}

	t.Setenv("MIN_INDEPENDENT_ACCOUNT_AGE", "21")
	if got := LoadMinimumIndependentAccountAge(); got != 21 {
		t.Fatalf("configured minimum age=%d", got)
	}
}
