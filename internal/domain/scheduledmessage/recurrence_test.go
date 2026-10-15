package scheduledmessage

import (
	"errors"
	"testing"
	"time"
)

func TestOnceHasNoNextOccurrence(t *testing.T) {
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	got, err := NextOccurrence(RecurrenceOnce, start, start, "UTC")
	if err != nil || got != nil {
		t.Fatalf("next=%v err=%v; want nil, nil", got, err)
	}
}

func TestDailyOccurrencePreservesJakartaClock(t *testing.T) {
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	got, err := NextOccurrence(RecurrenceDaily, start, start, "Asia/Jakarta")
	want := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	if err != nil || got == nil || !got.Equal(want) {
		t.Fatalf("next=%v err=%v want=%v", got, err, want)
	}
}

func TestDailyOccurrencePreservesClockAcrossDST(t *testing.T) {
	start := time.Date(2026, 3, 7, 14, 30, 0, 0, time.UTC) // 9:30 AM EST

	got, err := NextOccurrence(RecurrenceDaily, start, start, "America/New_York")
	want := time.Date(2026, 3, 8, 13, 30, 0, 0, time.UTC) // 9:30 AM EDT
	if err != nil || got == nil || !got.Equal(want) {
		t.Fatalf("next=%v err=%v want=%v", got, err, want)
	}
}

func TestWeeklyOccurrencePreservesClockAcrossDST(t *testing.T) {
	start := time.Date(2026, 10, 25, 13, 15, 0, 0, time.UTC) // 9:15 AM EDT

	got, err := NextOccurrence(RecurrenceWeekly, start, start, "America/New_York")
	want := time.Date(2026, 11, 1, 14, 15, 0, 0, time.UTC) // 9:15 AM EST
	if err != nil || got == nil || !got.Equal(want) {
		t.Fatalf("next=%v err=%v want=%v", got, err, want)
	}
}

func TestDailyOccurrenceSkipsBacklog(t *testing.T) {
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	after := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

	got, err := NextOccurrence(RecurrenceDaily, start, after, "UTC")
	want := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if err != nil || got == nil || !got.Equal(want) {
		t.Fatalf("next=%v err=%v want=%v", got, err, want)
	}
}

func TestNextOccurrenceRejectsInvalidTimezone(t *testing.T) {
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	if _, err := NextOccurrence(RecurrenceDaily, start, start, "Mars/Olympus"); !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("invalid timezone error=%v", err)
	}
}

func TestNextOccurrenceRejectsMachineLocalTimezone(t *testing.T) {
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	if _, err := NextOccurrence(RecurrenceDaily, start, start, "Local"); !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("machine-local timezone error=%v", err)
	}
}

func TestNextOccurrenceRejectsAmbiguousLocalTime(t *testing.T) {
	start := time.Date(2026, 10, 31, 5, 30, 0, 0, time.UTC) // 1:30 AM EDT

	if _, err := NextOccurrence(RecurrenceDaily, start, start, "America/New_York"); !errors.Is(err, ErrAmbiguousLocalTime) {
		t.Fatalf("ambiguous local time error=%v", err)
	}
}

func TestNextOccurrenceRejectsNonexistentLocalTime(t *testing.T) {
	start := time.Date(2026, 3, 7, 7, 30, 0, 0, time.UTC) // 2:30 AM EST

	if _, err := NextOccurrence(RecurrenceDaily, start, start, "America/New_York"); !errors.Is(err, ErrNonexistentLocalTime) {
		t.Fatalf("nonexistent local time error=%v", err)
	}
}
