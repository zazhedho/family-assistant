package servicescheduledmessage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
	interfacenotification "family-assistant/internal/interfaces/notification"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	"family-assistant/pkg/config"
)

func TestRunOnceSendsFixedMessageWithManagementContext(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, "hello"))
	sender := &testSender{}
	scheduler := newTestScheduler(t, repo, nil, nil, sender, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if sender.calls() != 1 {
		t.Fatalf("send calls = %d, want 1", sender.calls())
	}
	message := sender.messages[0]
	for _, part := range []string{"member-sender", "schedule-1", "space-1", "unsubscribe", "hello"} {
		if !strings.Contains(message, part) {
			t.Errorf("message %q missing %q", message, part)
		}
	}
	schedule, delivery := repo.state("schedule-1", "delivery-1")
	if delivery.Status != domainscheduledmessage.DeliveryStatusSent || schedule.Status != domainscheduledmessage.ScheduleStatusSent {
		t.Fatalf("final state = %s/%s, want SENT/SENT", delivery.Status, schedule.Status)
	}
}

func TestRunOnceAllowsFixedTextAboveGeneratorOutputBound(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	content := strings.Repeat("f", 40)
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, content))
	sender := &testSender{}
	cfg := testScheduledConfig()
	cfg.MaxOutputBytes = 32
	cfg.MaxTextBytes = 40
	scheduler := newTestScheduler(t, repo, nil, nil, sender, cfg, clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	schedule, delivery := repo.state("schedule-1", "delivery-1")
	if sender.calls() != 1 || delivery.Status != domainscheduledmessage.DeliveryStatusSent || schedule.Status != domainscheduledmessage.ScheduleStatusSent {
		t.Fatalf("fixed content of %d bytes with output bound %d: sends=%d state=%s/%s; want one send and SENT/SENT", len(content), cfg.MaxOutputBytes, sender.calls(), delivery.Status, schedule.Status)
	}
	if !strings.Contains(sender.messages[0], content) {
		t.Fatalf("sent message %q does not contain fixed content", sender.messages[0])
	}
}

func TestRunOnceRetriesWhenGeneratedOutputExceedsOutputBound(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme"))
	generator := &testGenerator{outputs: []string{strings.Repeat("a", 21)}}
	sender := &testSender{}
	cfg := testScheduledConfig()
	cfg.MaxOutputBytes = 20
	cfg.MaxTextBytes = 40
	scheduler := newTestScheduler(t, repo, nil, generator, sender, cfg, clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if generator.calls() != 1 || sender.calls() != 0 || delivery.GenerationAttempts != 1 || delivery.Status != domainscheduledmessage.DeliveryStatusPending || delivery.NextAttemptAt == nil {
		t.Fatalf("oversized generated output state: generator=%d sends=%d attempts=%d status=%s next=%v; want one generation, no send, and pending retry", generator.calls(), sender.calls(), delivery.GenerationAttempts, delivery.Status, delivery.NextAttemptAt)
	}
}

func TestRunOnceCancelsFixedTextAboveInputBound(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, strings.Repeat("f", 17)))
	sender := &testSender{}
	cfg := testScheduledConfig()
	cfg.MaxOutputBytes = 32
	cfg.MaxTextBytes = 16
	scheduler := newTestScheduler(t, repo, nil, nil, sender, cfg, clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	schedule, delivery := repo.state("schedule-1", "delivery-1")
	if sender.calls() != 0 || delivery.Status != domainscheduledmessage.DeliveryStatusCancelled || schedule.Status != domainscheduledmessage.ScheduleStatusCancelled {
		t.Fatalf("fixed content above input bound: sends=%d state=%s/%s; want no send and CANCELLED/CANCELLED", sender.calls(), delivery.Status, schedule.Status)
	}
}

func TestRunOnceSkipsGenerationWhenReservationLockWaitLeavesTooLittleLease(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme"))
	repo.afterGenerationReserve = func() { clock.set(now.Add(119 * time.Second)) }
	generator := &testGenerator{outputs: []string{"must not run"}}
	scheduler := newTestScheduler(t, repo, nil, generator, &testSender{}, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if generator.calls() != 0 || delivery.GenerationAttempts != 1 || delivery.LeaseToken == nil {
		t.Fatalf("generation calls=%d, reserved=%d, lease=%v; want no call, one persisted reservation, live claim retained", generator.calls(), delivery.GenerationAttempts, delivery.LeaseToken)
	}
}

func TestRunOnceSkipsSendWhenReservationLockWaitLeavesTooLittleLease(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, "hello"))
	repo.afterDeliveryReserve = func() { clock.set(now.Add(115 * time.Second)) }
	sender := &testSender{}
	scheduler := newTestScheduler(t, repo, nil, nil, sender, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if sender.calls() != 0 || delivery.DeliveryAttempts != 1 || delivery.LeaseToken == nil {
		t.Fatalf("send calls=%d, reserved=%d, lease=%v; want no call, one persisted reservation, live claim retained", sender.calls(), delivery.DeliveryAttempts, delivery.LeaseToken)
	}
}

func TestRunOnceCancelsOccurrenceWhenSpaceIsInactiveAndReactivationCannotResurrectIt(t *testing.T) {
	for _, stage := range []string{"before_generation", "after_generation", "final_dispatch_gate"} {
		t.Run(stage, func(t *testing.T) {
			now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
			clock := &testClock{now: now}
			repo := newMemoryScheduledRepository()
			mode := domainscheduledmessage.ContentModeAI
			if stage == "final_dispatch_gate" {
				mode = domainscheduledmessage.ContentModeFixed
			}
			repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, mode, "theme"))
			generator := &testGenerator{outputs: []string{"generated"}}
			if stage == "before_generation" {
				repo.setSpaceActive("space-1", false)
			}
			if stage == "after_generation" {
				generator.afterCall = func() { repo.setSpaceActive("space-1", false) }
			}
			if stage == "final_dispatch_gate" {
				repo.afterDeliveryReserve = func() { repo.setSpaceActive("space-1", false) }
			}
			sender := &testSender{}
			scheduler := newTestScheduler(t, repo, nil, generator, sender, testScheduledConfig(), clock)
			if err := scheduler.RunOnce(context.Background(), now); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			schedule, delivery := repo.state("schedule-1", "delivery-1")
			if schedule.Status != domainscheduledmessage.ScheduleStatusCancelled || delivery.Status != domainscheduledmessage.DeliveryStatusCancelled {
				t.Fatalf("inactive Space state = %s/%s; want CANCELLED/CANCELLED", schedule.Status, delivery.Status)
			}
			repo.setSpaceActive("space-1", true)
			if err := scheduler.RunOnce(context.Background(), now.Add(3*time.Minute)); err != nil {
				t.Fatalf("RunOnce() after Space reactivation = %v", err)
			}
			if sender.calls() != 0 || (stage != "before_generation" && stage != "final_dispatch_gate" && generator.calls() != 1) {
				t.Fatalf("external calls after inactive Space transition: generator=%d sender=%d", generator.calls(), sender.calls())
			}
		})
	}
}

func TestRunOncePersistsGeneratedMessageAndReusesItAfterSendFailure(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "warm theme"))
	repo.history["schedule-1"] = []string{"older 1", "older 2"}
	generator := &testGenerator{outputs: []string{"generated note"}}
	sender := &testSender{failures: []error{errors.New("bridge unavailable")}}
	cfg := testScheduledConfig()
	cfg.DeliveryMaxRetries = 1
	scheduler := newTestScheduler(t, repo, nil, generator, sender, cfg, clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if delivery.ContentSnapshot == nil || *delivery.ContentSnapshot != "generated note" {
		t.Fatalf("persisted snapshot = %v, want generated note before send retry", delivery.ContentSnapshot)
	}
	if delivery.NextAttemptAt == nil || delivery.LeaseToken != nil {
		t.Fatalf("retry state = next %v, lease %v; want backoff with released lease", delivery.NextAttemptAt, delivery.LeaseToken)
	}
	clock.set(*delivery.NextAttemptAt)
	if err := scheduler.RunOnce(context.Background(), *delivery.NextAttemptAt); err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	if generator.calls() != 1 || sender.calls() != 2 {
		t.Fatalf("calls = generator %d, sender %d; want 1 and 2", generator.calls(), sender.calls())
	}
	if sender.messages[1] == "" || !strings.Contains(sender.messages[1], "generated note") {
		t.Fatalf("retry message = %q, want saved generated note", sender.messages[1])
	}
	if input := generator.inputs[0]; input.Theme != "warm theme" || input.RecipientName != "Recipient" || len(input.RecentMessages) != 2 {
		t.Fatalf("generation input = %+v, want schedule context and bounded history", input)
	}
}

func TestRunOnceDoesNotExceedReservedGenerationBudgetAfterRestart(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	row := testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme")
	row.delivery.GenerationAttempts = 4
	repo.addSchedule(row)
	generator := &testGenerator{outputs: []string{"must not run"}}
	scheduler := newTestScheduler(t, repo, nil, generator, &testSender{}, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	schedule, delivery := repo.state("schedule-1", "delivery-1")
	if generator.calls() != 0 {
		t.Fatalf("generation calls = %d, want 0 after fourth reserved attempt", generator.calls())
	}
	if delivery.GenerationAttempts != 4 || delivery.Status != domainscheduledmessage.DeliveryStatusFailed || schedule.Status != domainscheduledmessage.ScheduleStatusFailed {
		t.Fatalf("exhausted state = attempts %d, delivery %s, schedule %s", delivery.GenerationAttempts, delivery.Status, schedule.Status)
	}
}

func TestRunOnceExhaustsGenerationRetriesAndFinalizesByRecurrence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		recurrence domainscheduledmessage.Recurrence
		wantState  domainscheduledmessage.DeliveryStatus
		wantSched  domainscheduledmessage.ScheduleStatus
	}{
		{name: "once", recurrence: domainscheduledmessage.RecurrenceOnce, wantState: domainscheduledmessage.DeliveryStatusFailed, wantSched: domainscheduledmessage.ScheduleStatusFailed},
		{name: "daily", recurrence: domainscheduledmessage.RecurrenceDaily, wantState: domainscheduledmessage.DeliveryStatusSkipped, wantSched: domainscheduledmessage.ScheduleStatusActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
			clock := &testClock{now: now}
			repo := newMemoryScheduledRepository()
			repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, tc.recurrence, domainscheduledmessage.ContentModeAI, "theme"))
			generator := &testGenerator{errors: []error{errors.New("failed"), errors.New("failed"), errors.New("failed"), errors.New("failed"), errors.New("must not run")}}
			cfg := testScheduledConfig()
			cfg.GenerationMaxRetries = 3
			scheduler := newTestScheduler(t, repo, nil, generator, &testSender{}, cfg, clock)

			for attempt := 1; attempt <= 4; attempt++ {
				if err := scheduler.RunOnce(context.Background(), clock.time()); err != nil {
					t.Fatalf("RunOnce() attempt %d error = %v", attempt, err)
				}
				_, delivery := repo.state("schedule-1", "delivery-1")
				if delivery.GenerationAttempts != attempt {
					t.Fatalf("reserved generation attempts = %d, want %d", delivery.GenerationAttempts, attempt)
				}
				if attempt < 4 {
					if delivery.Status != domainscheduledmessage.DeliveryStatusPending || delivery.NextAttemptAt == nil {
						t.Fatalf("attempt %d state = %s, next %v; want pending retry", attempt, delivery.Status, delivery.NextAttemptAt)
					}
					clock.set(*delivery.NextAttemptAt)
				}
			}

			schedule, delivery := repo.state("schedule-1", "delivery-1")
			if generator.calls() != 4 || delivery.Status != tc.wantState || schedule.Status != tc.wantSched {
				t.Fatalf("calls/state = %d/%s/%s; want 4/%s/%s", generator.calls(), delivery.Status, schedule.Status, tc.wantState, tc.wantSched)
			}
			if tc.recurrence == domainscheduledmessage.RecurrenceDaily {
				next := repo.next["schedule-1"]
				if next == nil || !next.ScheduledAt.After(clock.time()) {
					t.Fatalf("next recurrence = %v; want a future occurrence", next)
				}
			}
		})
	}
}

func TestRunOnceExhaustsDeliveryRetriesWithoutResendingAfterBudget(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, "hello"))
	sender := &testSender{failures: []error{errors.New("failed"), errors.New("failed"), errors.New("failed"), errors.New("failed"), errors.New("must not run")}}
	cfg := testScheduledConfig()
	cfg.DeliveryMaxRetries = 3
	scheduler := newTestScheduler(t, repo, nil, nil, sender, cfg, clock)

	for attempt := 1; attempt <= 4; attempt++ {
		if err := scheduler.RunOnce(context.Background(), clock.time()); err != nil {
			t.Fatalf("RunOnce() attempt %d error = %v", attempt, err)
		}
		_, delivery := repo.state("schedule-1", "delivery-1")
		if delivery.DeliveryAttempts != attempt {
			t.Fatalf("reserved delivery attempts = %d, want %d", delivery.DeliveryAttempts, attempt)
		}
		if attempt < 4 {
			if delivery.Status != domainscheduledmessage.DeliveryStatusPending || delivery.NextAttemptAt == nil {
				t.Fatalf("attempt %d state = %s, next %v; want pending retry", attempt, delivery.Status, delivery.NextAttemptAt)
			}
			clock.set(*delivery.NextAttemptAt)
		}
	}
	schedule, delivery := repo.state("schedule-1", "delivery-1")
	if sender.calls() != 4 || delivery.Status != domainscheduledmessage.DeliveryStatusFailed || schedule.Status != domainscheduledmessage.ScheduleStatusFailed {
		t.Fatalf("calls/state = %d/%s/%s; want 4/FAILED/FAILED", sender.calls(), delivery.Status, schedule.Status)
	}
}

func TestRunOnceDoesNotResendAfterAcceptedButUncertainFinalize(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, "hello"))
	repo.uncertainAfter = true
	sender := &testSender{}
	scheduler := newTestScheduler(t, repo, nil, nil, sender, testScheduledConfig(), clock)

	err := scheduler.RunOnce(context.Background(), now)
	if !errors.Is(err, interfacescheduledmessage.ErrDispatchOutcomeUncertain) {
		t.Fatalf("RunOnce() error = %v, want uncertain accepted delivery", err)
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if sender.calls() != 1 || delivery.DeliveryAttempts != 1 || delivery.LeaseToken == nil {
		t.Fatalf("after uncertain send = calls %d, attempts %d, lease %v; want one accepted attempt and preserved lease", sender.calls(), delivery.DeliveryAttempts, delivery.LeaseToken)
	}
	clock.set(now.Add(time.Second))
	if err := scheduler.RunOnce(context.Background(), clock.time()); err != nil {
		t.Fatalf("RunOnce() before lease expiry error = %v", err)
	}
	if sender.calls() != 1 {
		t.Fatalf("send calls after uncertain outcome = %d, want no immediate resend", sender.calls())
	}
}

func TestRunOnceCancelsWhenCreatorPermissionIsRevokedDuringGeneration(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme"))
	generator := &testGenerator{outputs: []string{"generated"}, afterCall: func() { repo.targetError("delivery-1", interfacescheduledmessage.ErrInvalidTarget) }}
	sender := &testSender{}
	scheduler := newTestScheduler(t, repo, nil, generator, sender, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	schedule, delivery := repo.state("schedule-1", "delivery-1")
	if generator.calls() != 1 || sender.calls() != 0 || delivery.Status != domainscheduledmessage.DeliveryStatusCancelled || schedule.Status != domainscheduledmessage.ScheduleStatusCancelled {
		t.Fatalf("permission-revoked state = generate %d/send %d/delivery %s/schedule %s", generator.calls(), sender.calls(), delivery.Status, schedule.Status)
	}
}

func TestMemoryRepositoryRejectsCrossClaimAndStaleAttemptReservation(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, "hello"))
	first, err := repo.ClaimDue(context.Background(), now, time.Minute, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim = %d rows, error %v", len(first), err)
	}
	second, err := repo.ClaimDue(context.Background(), now.Add(time.Second), time.Minute, 1)
	if err != nil || len(second) != 0 {
		t.Fatalf("concurrent claim = %d rows, error %v; want active lease exclusion", len(second), err)
	}
	reclaimed, err := repo.ClaimDue(context.Background(), now.Add(time.Minute+time.Second), time.Minute, 1)
	if err != nil || len(reclaimed) != 1 || pointerStringValue(reclaimed[0].LeaseToken) == pointerStringValue(first[0].LeaseToken) {
		t.Fatalf("reclaim = %#v, error %v; want a new token after expiry", reclaimed, err)
	}
	if err := repo.ReserveDeliveryAttempt(context.Background(), &first[0], now.Add(time.Minute+time.Second), 2); !errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
		t.Fatalf("stale token reservation error = %v, want claim conflict", err)
	}
	if err := repo.ReserveDeliveryAttempt(context.Background(), &reclaimed[0], now.Add(time.Minute+time.Second), 2); err != nil {
		t.Fatalf("current token reservation error = %v", err)
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if delivery.DeliveryAttempts != 1 {
		t.Fatalf("delivery attempts after stale/current token = %d, want 1", delivery.DeliveryAttempts)
	}
}

func TestRunOnceUsesAcceptanceTimeForNextCalendarOccurrence(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 14, 0, 1, 0, 0, loc)
	now := time.Date(2026, 10, 14, 23, 59, 0, 0, loc)
	accepted := time.Date(2026, 10, 15, 0, 2, 0, 0, loc)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", start, domainscheduledmessage.RecurrenceDaily, domainscheduledmessage.ContentModeFixed, "hello"))
	sender := &testSender{onSend: func() { clock.set(accepted) }}
	scheduler := newTestScheduler(t, repo, nil, nil, sender, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	next := repo.next["schedule-1"]
	want := time.Date(2026, 10, 16, 0, 1, 0, 0, loc)
	if next == nil || !next.ScheduledAt.Equal(want) {
		t.Fatalf("next occurrence = %v, want %v after actual acceptance", next, want)
	}
}

func TestRunOnceCancelsWhenTargetInvalidatedDuringGeneration(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme"))
	generator := &testGenerator{outputs: []string{"generated note"}, afterCall: func() { repo.cancelExternally("schedule-1", "delivery-1") }}
	sender := &testSender{}
	scheduler := newTestScheduler(t, repo, nil, generator, sender, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if sender.calls() != 0 {
		t.Fatalf("send calls = %d after cancellation during generation, want 0", sender.calls())
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if delivery.Status != domainscheduledmessage.DeliveryStatusCancelled {
		t.Fatalf("delivery status = %s, want CANCELLED", delivery.Status)
	}
}

func TestRunOnceDoesNotRetryOrSendWhenParentContextIsCancelledDuringGeneration(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme"))
	started := make(chan struct{}, 1)
	generator := &testGenerator{outputs: []string{"generated"}, gate: make(chan struct{}), started: started}
	sender := &testSender{}
	scheduler := newTestScheduler(t, repo, nil, generator, sender, testScheduledConfig(), clock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- scheduler.RunOnce(ctx, now) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("generator did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOnce() error = %v, want benign parent cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunOnce() did not stop after parent cancellation")
	}
	_, delivery := repo.state("schedule-1", "delivery-1")
	if generator.calls() != 1 || sender.calls() != 0 || delivery.GenerationAttempts != 1 || delivery.DeliveryAttempts != 0 || delivery.Status != domainscheduledmessage.DeliveryStatusPending {
		t.Fatalf("cancelled generation = calls %d/send %d/generation attempts %d/delivery attempts %d/status %s; want one reservation, no send or retry, pending", generator.calls(), sender.calls(), delivery.GenerationAttempts, delivery.DeliveryAttempts, delivery.Status)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, event := range repo.events {
		if event == "retry" || event == "dispatch_finalize" {
			t.Fatalf("parent cancellation recorded unexpected repository event %q", event)
		}
	}
}

func TestRunOnceDoesNotSendWhenUnsubscribeOrUpdateWinsDuringGeneration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*memoryScheduledRepository)
	}{
		{name: "unsubscribe", mutate: func(repo *memoryScheduledRepository) { repo.unsubscribeExternally("schedule-1", "delivery-1") }},
		{name: "update", mutate: func(repo *memoryScheduledRepository) { repo.updateExternally("schedule-1", "delivery-1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
			clock := &testClock{now: now}
			repo := newMemoryScheduledRepository()
			repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme"))
			generator := &testGenerator{outputs: []string{"generated"}, afterCall: func() { tc.mutate(repo) }}
			sender := &testSender{}
			scheduler := newTestScheduler(t, repo, nil, generator, sender, testScheduledConfig(), clock)

			if err := scheduler.RunOnce(context.Background(), now); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			schedule, delivery := repo.state("schedule-1", "delivery-1")
			if generator.calls() != 1 || sender.calls() != 0 || delivery.Status != domainscheduledmessage.DeliveryStatusCancelled {
				t.Fatalf("mutation race = generate %d/send %d/delivery %s", generator.calls(), sender.calls(), delivery.Status)
			}
			if tc.name == "unsubscribe" && schedule.Status != domainscheduledmessage.ScheduleStatusUnsubscribed {
				t.Fatalf("unsubscribe race schedule = %s", schedule.Status)
			}
			if tc.name == "update" && (schedule.Status != domainscheduledmessage.ScheduleStatusActive || schedule.Content != "updated theme") {
				t.Fatalf("update race schedule = %s/%q", schedule.Status, schedule.Content)
			}
		})
	}
}

func TestRunOnceCancelsAnInvalidCurrentTargetWithoutRetrying(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, "hello"))
	repo.targetError("delivery-1", interfacescheduledmessage.ErrInvalidTarget)
	sender := &testSender{}
	scheduler := newTestScheduler(t, repo, nil, nil, sender, testScheduledConfig(), clock)

	if err := scheduler.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	schedule, delivery := repo.state("schedule-1", "delivery-1")
	if sender.calls() != 0 || delivery.DeliveryAttempts != 0 {
		t.Fatalf("invalid target caused %d sends and %d reserved attempts", sender.calls(), delivery.DeliveryAttempts)
	}
	if delivery.Status != domainscheduledmessage.DeliveryStatusCancelled || schedule.Status != domainscheduledmessage.ScheduleStatusCancelled {
		t.Fatalf("invalid target state = %s/%s, want CANCELLED/CANCELLED", delivery.Status, schedule.Status)
	}
}

func TestRunOnceStartsAllClaimedGenerationWithoutQueueing(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	repo := newMemoryScheduledRepository()
	repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme one"))
	repo.addSchedule(testSchedule("schedule-2", "delivery-2", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeAI, "theme two"))
	gate := make(chan struct{})
	started := make(chan struct{}, 2)
	generator := &testGenerator{outputs: []string{"note"}, gate: gate, started: started}
	scheduler := newTestScheduler(t, repo, nil, generator, &testSender{}, testScheduledConfig(), clock)
	done := make(chan error, 1)
	go func() { done <- scheduler.RunOnce(context.Background(), now) }()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(gate)
			t.Fatal("claimed generation was queued behind another generator")
		}
	}
	close(gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunOnce() did not finish after generators were released")
	}
}

func TestReconcileIndexDoesNotAcknowledgeFailedOrStaleSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	index := newTestDueIndex()
	repo := newMemoryScheduledRepository()
	repo.pendingErr = errors.New("snapshot failed")
	scheduler := newTestScheduler(t, repo, index, nil, &testSender{}, testScheduledConfig(), clock)
	if err := scheduler.reconcileIndex(context.Background()); err == nil {
		t.Fatal("reconcileIndex() error = nil, want snapshot failure")
	}
	if index.markCalls != 0 || !index.degraded {
		t.Fatalf("failed snapshot ack count = %d, degraded = %v", index.markCalls, index.degraded)
	}

	repo.pendingErr = nil
	repo.onPendingList = func() {
		index.mu.Lock()
		index.events = append(index.events, "pending_snapshot")
		index.mu.Unlock()
		_ = index.Upsert(context.Background(), "concurrent-create", now)
	}
	if err := scheduler.reconcileIndex(context.Background()); err != nil {
		t.Fatalf("second reconcileIndex() error = %v", err)
	}
	if !index.degraded {
		t.Fatal("stale reconciliation acknowledgement cleared dirty index")
	}
	if !orderedSubsequence(index.events, []string{"version", "all_ids", "pending_snapshot", "upsert_many", "remove_many", "mark"}) {
		t.Fatalf("repair event order = %v", index.events)
	}
}

func TestReconcileIndexDoesNotAcknowledgeIndexRepairFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testDueIndex)
	}{
		{name: "read ids", setup: func(index *testDueIndex) { index.allErr = errors.New("read failed") }},
		{name: "upsert snapshot", setup: func(index *testDueIndex) { index.upsertManyErr = errors.New("upsert failed") }},
		{name: "remove stale ids", setup: func(index *testDueIndex) {
			index.ids["stale"] = time.Now()
			index.removeManyErr = errors.New("remove failed")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index := newTestDueIndex()
			tc.setup(index)
			repo := newMemoryScheduledRepository()
			scheduler := newTestScheduler(t, repo, index, nil, &testSender{}, testScheduledConfig(), &testClock{now: time.Now()})
			if err := scheduler.reconcileIndex(context.Background()); err == nil {
				t.Fatal("reconcileIndex() error = nil, want index repair failure")
			}
			if index.markCalls != 0 || !index.degraded {
				t.Fatalf("failed repair acknowledgement = %d, degraded = %v", index.markCalls, index.degraded)
			}
		})
	}
}

func TestReconcileIndexStaysDirtyAfterConcurrentCreateUpdateOrCancel(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*testDueIndex, time.Time)
	}{
		{name: "create", mutate: func(index *testDueIndex, at time.Time) { _ = index.Upsert(context.Background(), "created", at) }},
		{name: "update", mutate: func(index *testDueIndex, at time.Time) {
			_ = index.Upsert(context.Background(), "updated", at.Add(time.Minute))
		}},
		{name: "cancel", mutate: func(index *testDueIndex, _ time.Time) { _ = index.Remove(context.Background(), "cancelled") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index := newTestDueIndex()
			repo := newMemoryScheduledRepository()
			repo.onPendingList = func() { tc.mutate(index, now) }
			scheduler := newTestScheduler(t, repo, index, nil, &testSender{}, testScheduledConfig(), &testClock{now: now})
			if err := scheduler.reconcileIndex(context.Background()); err != nil {
				t.Fatalf("reconcileIndex() error = %v", err)
			}
			if !index.degraded {
				t.Fatal("concurrent change was acknowledged from a stale snapshot")
			}
		})
	}
}

func TestSchedulerRetryAndFinalizeGuardIndexAcrossCommitToCacheGap(t *testing.T) {
	for _, operation := range []string{"retry", "finalize"} {
		t.Run(operation, func(t *testing.T) {
			now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
			clock := &testClock{now: now}
			repo := newMemoryScheduledRepository()
			repo.addSchedule(testSchedule("schedule-1", "delivery-1", now, domainscheduledmessage.RecurrenceOnce, domainscheduledmessage.ContentModeFixed, "hello"))
			token, expires := "lease-live", now.Add(time.Minute)
			repo.mu.Lock()
			repo.deliveries["delivery-1"].LeaseToken, repo.deliveries["delivery-1"].LeaseExpiresAt = &token, &expires
			repo.mu.Unlock()
			index := newTestDueIndex()
			committed, releaseCommit := make(chan struct{}), make(chan struct{})
			pauseCommit := func() { close(committed); <-releaseCommit }
			if operation == "retry" {
				repo.afterRetry = pauseCommit
			} else {
				repo.afterFinalize = pauseCommit
			}
			markEntered, releaseMark := make(chan struct{}), make(chan struct{})
			index.beforeMark = func() { close(markEntered); <-releaseMark }
			scheduler := newTestScheduler(t, repo, index, nil, &testSender{}, testScheduledConfig(), clock)
			reconciled := make(chan error, 1)
			go func() { reconciled <- scheduler.reconcileIndex(context.Background()) }()
			<-markEntered
			schedule, delivery := repo.state("schedule-1", "delivery-1")
			mutation := make(chan error, 1)
			go func() {
				if operation == "retry" {
					mutation <- scheduler.retryOrFinish(context.Background(), schedule, delivery, false)
					return
				}
				mutation <- scheduler.finishDeliveryFailureAt(context.Background(), schedule, delivery, now)
			}()
			<-committed
			close(releaseMark)
			if err := <-reconciled; err != nil {
				t.Fatalf("reconcileIndex() error = %v", err)
			}
			index.mu.Lock()
			degraded := index.degraded
			index.mu.Unlock()
			close(releaseCommit)
			if err := <-mutation; err != nil {
				t.Fatalf("scheduler %s mutation error = %v", operation, err)
			}
			if !degraded {
				t.Fatalf("reconciliation acknowledged stale snapshot across scheduler %s DB commit/cache gap", operation)
			}
		})
	}
}

func TestNewScheduledMessageSchedulerValidatesDirectConfig(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	for _, tc := range []struct {
		name string
		edit func(*config.ScheduledMessageConfig)
	}{
		{name: "batch exceeds concurrency", edit: func(cfg *config.ScheduledMessageConfig) { cfg.BatchSize, cfg.Concurrency = 3, 2 }},
		{name: "lease cannot cover bounded operation", edit: func(cfg *config.ScheduledMessageConfig) { cfg.Lease = time.Second }},
		{name: "retry budget above three", edit: func(cfg *config.ScheduledMessageConfig) { cfg.GenerationMaxRetries = 4 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testScheduledConfig()
			tc.edit(&cfg)
			if _, err := NewScheduledMessageScheduler(newMemoryScheduledRepository(), nil, nil, &testSender{}, cfg, dto.ScheduledMessageSchedulerOptions{Now: clock.time}); err == nil {
				t.Fatal("constructor error = nil for unsafe direct config")
			}
		})
	}
}

func newTestScheduler(t *testing.T, repo *memoryScheduledRepository, index interfacescheduledmessage.DueIndex, generator interfacescheduledmessage.Generator, sender interfacenotification.NotificationSender, cfg config.ScheduledMessageConfig, clock *testClock) *ScheduledMessageScheduler {
	t.Helper()
	scheduler, err := NewScheduledMessageScheduler(repo, index, generator, sender, cfg, dto.ScheduledMessageSchedulerOptions{Now: clock.time})
	if err != nil {
		t.Fatalf("NewScheduledMessageScheduler() error = %v", err)
	}
	return scheduler
}

func testScheduledConfig() config.ScheduledMessageConfig {
	return config.ScheduledMessageConfig{
		Enabled: true,
		Scheduler: config.ReminderSchedulerConfig{
			WhatsAppBridgeURL: "http://bridge.test", Interval: time.Minute, DatabaseFallbackInterval: time.Second,
			IndexReconcileInterval: time.Hour, IndexBatchSize: 10,
		},
		GeneratorTimeout: 2 * time.Second, MaxResponseBytes: 4096, MaxOutputBytes: 2000,
		GenerationMaxRetries: 1, GenerationRetryBackoff: 30 * time.Second,
		DeliveryMaxRetries: 1, DeliveryRetryBackoff: 30 * time.Second,
		Lease: 2 * time.Minute, BatchSize: 2, Concurrency: 2, MaxTextBytes: 2000, MaxListRows: 100,
	}
}

func testSchedule(scheduleID, deliveryID string, scheduledAt time.Time, recurrence domainscheduledmessage.Recurrence, mode domainscheduledmessage.ContentMode, content string) scheduledState {
	contentSnapshot := content
	delivery := &domainscheduledmessage.Delivery{
		ID: deliveryID, ScheduledMessageID: scheduleID, ScheduledAt: scheduledAt,
		NextAttemptAt: schedTestTimePointer(scheduledAt), Status: domainscheduledmessage.DeliveryStatusPending,
	}
	if mode == domainscheduledmessage.ContentModeFixed {
		delivery.ContentSnapshot = &contentSnapshot
	}
	return scheduledState{
		schedule: &domainscheduledmessage.Schedule{
			ID: scheduleID, SpaceID: "space-1", CreatedByMemberID: "member-sender", Target: domainscheduledmessage.TargetDM,
			RecipientMemberID: schedTestStringPointer("member-recipient"), ContentMode: mode, Content: content,
			Recurrence: recurrence, Timezone: "UTC", StartAt: scheduledAt, NextScheduledAt: schedTestTimePointer(scheduledAt),
			Status: domainscheduledmessage.ScheduleStatusActive,
		},
		delivery: delivery,
	}
}

type scheduledState struct {
	schedule *domainscheduledmessage.Schedule
	delivery *domainscheduledmessage.Delivery
}

type memoryScheduledRepository struct {
	mu                     sync.Mutex
	schedules              map[string]*domainscheduledmessage.Schedule
	deliveries             map[string]*domainscheduledmessage.Delivery
	next                   map[string]*domainscheduledmessage.Delivery
	history                map[string][]string
	targetErr              map[string]error
	inactiveSpaces         map[string]bool
	afterGenerationReserve func()
	afterDeliveryReserve   func()
	afterRetry             func()
	afterFinalize          func()
	pendingErr             error
	onPendingList          func()
	claimCount             int
	events                 []string
	uncertainAfter         bool
}

func newMemoryScheduledRepository() *memoryScheduledRepository {
	return &memoryScheduledRepository{
		schedules: make(map[string]*domainscheduledmessage.Schedule), deliveries: make(map[string]*domainscheduledmessage.Delivery),
		next: make(map[string]*domainscheduledmessage.Delivery), history: make(map[string][]string), targetErr: make(map[string]error), inactiveSpaces: make(map[string]bool),
	}
}

func (r *memoryScheduledRepository) addSchedule(state scheduledState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schedules[state.schedule.ID] = cloneSchedule(state.schedule)
	r.deliveries[state.delivery.ID] = cloneDelivery(state.delivery)
}

func (r *memoryScheduledRepository) state(scheduleID, deliveryID string) (*domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneSchedule(r.schedules[scheduleID]), cloneDelivery(r.deliveries[deliveryID])
}

func (r *memoryScheduledRepository) cancelExternally(scheduleID, deliveryID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule := r.schedules[scheduleID]
	delivery := r.deliveries[deliveryID]
	schedule.Status = domainscheduledmessage.ScheduleStatusCancelled
	schedule.NextScheduledAt = nil
	delivery.Status = domainscheduledmessage.DeliveryStatusCancelled
	delivery.LeaseToken, delivery.LeaseExpiresAt = nil, nil
}

func (r *memoryScheduledRepository) unsubscribeExternally(scheduleID, deliveryID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule := r.schedules[scheduleID]
	delivery := r.deliveries[deliveryID]
	schedule.Status = domainscheduledmessage.ScheduleStatusUnsubscribed
	schedule.NextScheduledAt = nil
	delivery.Status = domainscheduledmessage.DeliveryStatusCancelled
	delivery.LeaseToken, delivery.LeaseExpiresAt = nil, nil
}

func (r *memoryScheduledRepository) updateExternally(scheduleID, deliveryID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule := r.schedules[scheduleID]
	delivery := r.deliveries[deliveryID]
	schedule.Content = "updated theme"
	delivery.Status = domainscheduledmessage.DeliveryStatusCancelled
	delivery.LeaseToken, delivery.LeaseExpiresAt = nil, nil
}

func (r *memoryScheduledRepository) ListGeneratedHistory(_ context.Context, _, scheduleID string, limit int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit > 5 {
		limit = 5
	}
	messages := r.history[scheduleID]
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return append([]string(nil), messages...), nil
}

func (r *memoryScheduledRepository) ListPendingForIndex(context.Context) ([]domainscheduledmessage.Delivery, error) {
	r.mu.Lock()
	r.events = append(r.events, "pending_snapshot")
	if r.pendingErr != nil {
		err := r.pendingErr
		r.mu.Unlock()
		return nil, err
	}
	rows := make([]domainscheduledmessage.Delivery, 0, len(r.deliveries))
	for _, delivery := range r.deliveries {
		schedule := r.schedules[delivery.ScheduledMessageID]
		if schedule.Status == domainscheduledmessage.ScheduleStatusActive && delivery.Status == domainscheduledmessage.DeliveryStatusPending {
			rows = append(rows, *cloneDelivery(delivery))
		}
	}
	hook := r.onPendingList
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return rows, nil
}

func (r *memoryScheduledRepository) ClaimDue(_ context.Context, now time.Time, lease time.Duration, limit int) ([]domainscheduledmessage.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("claim:%d", limit))
	ids := make([]string, 0, len(r.deliveries))
	for id := range r.deliveries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	claimed := make([]domainscheduledmessage.Delivery, 0, limit)
	for _, id := range ids {
		delivery := r.deliveries[id]
		schedule := r.schedules[delivery.ScheduledMessageID]
		if len(claimed) == limit {
			break
		}
		if schedule.Status != domainscheduledmessage.ScheduleStatusActive || delivery.Status != domainscheduledmessage.DeliveryStatusPending || delivery.NextAttemptAt == nil || delivery.NextAttemptAt.After(now) {
			continue
		}
		if delivery.LeaseExpiresAt != nil && delivery.LeaseExpiresAt.After(now) {
			continue
		}
		r.claimCount++
		token := fmt.Sprintf("lease-%d", r.claimCount)
		expires := now.Add(lease)
		delivery.LeaseToken, delivery.LeaseExpiresAt = &token, &expires
		claimed = append(claimed, *cloneDelivery(delivery))
	}
	return claimed, nil
}

func (r *memoryScheduledRepository) LoadClaimed(_ context.Context, deliveryID, token string, now time.Time) (*domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery := r.deliveries[deliveryID]
	if !liveClaim(delivery, token, now) {
		return nil, nil, nil, nil, "", interfacescheduledmessage.ErrClaimConflict
	}
	schedule := r.schedules[delivery.ScheduledMessageID]
	if schedule.Status != domainscheduledmessage.ScheduleStatusActive {
		return nil, nil, nil, nil, "", interfacescheduledmessage.ErrClaimConflict
	}
	if r.inactiveSpaces[schedule.SpaceID] {
		return cloneSchedule(schedule), cloneDelivery(delivery), nil, nil, "", interfacescheduledmessage.ErrInvalidTarget
	}
	if err := r.targetErr[deliveryID]; err != nil {
		return cloneSchedule(schedule), cloneDelivery(delivery), nil, nil, "", err
	}
	identity := &domainidentity.ExternalIdentity{Provider: domainidentity.ProviderHermes, ExternalID: "15551234567@s.whatsapp.net", Status: domainidentity.StatusActive}
	return cloneSchedule(schedule), cloneDelivery(delivery), nil, identity, "Recipient", nil
}

func (r *memoryScheduledRepository) ReserveGenerationAttempt(_ context.Context, claimed *domainscheduledmessage.Delivery, at time.Time, maximum int) error {
	return r.reserve(claimed, at, maximum, true)
}

func (r *memoryScheduledRepository) ReserveDeliveryAttempt(_ context.Context, claimed *domainscheduledmessage.Delivery, at time.Time, maximum int) error {
	return r.reserve(claimed, at, maximum, false)
}

func (r *memoryScheduledRepository) reserve(claimed *domainscheduledmessage.Delivery, at time.Time, maximum int, generation bool) error {
	r.mu.Lock()
	row := r.deliveries[claimed.ID]
	if !liveClaim(row, pointerStringValue(claimed.LeaseToken), at) || r.schedules[row.ScheduledMessageID].Status != domainscheduledmessage.ScheduleStatusActive {
		r.mu.Unlock()
		return interfacescheduledmessage.ErrClaimConflict
	}
	count := &row.DeliveryAttempts
	if generation {
		count = &row.GenerationAttempts
	}
	if *count >= maximum {
		r.mu.Unlock()
		return interfacescheduledmessage.ErrAttemptLimitReached
	}
	*count++
	claimed.GenerationAttempts, claimed.DeliveryAttempts, claimed.UpdatedAt = row.GenerationAttempts, row.DeliveryAttempts, at
	hook := r.afterDeliveryReserve
	if generation {
		hook = r.afterGenerationReserve
	}
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (r *memoryScheduledRepository) PersistGeneratedContent(_ context.Context, claimed *domainscheduledmessage.Delivery, content string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.deliveries[claimed.ID]
	if !liveClaim(row, pointerStringValue(claimed.LeaseToken), at) {
		return interfacescheduledmessage.ErrClaimConflict
	}
	row.ContentSnapshot, claimed.ContentSnapshot = &content, &content
	row.UpdatedAt, claimed.UpdatedAt = at, at
	return nil
}

func (r *memoryScheduledRepository) RecordRetry(_ context.Context, claimed *domainscheduledmessage.Delivery, schedule *domainscheduledmessage.Schedule) error {
	r.mu.Lock()
	row := r.deliveries[claimed.ID]
	if !liveClaim(row, pointerStringValue(claimed.LeaseToken), claimed.UpdatedAt) {
		r.mu.Unlock()
		return interfacescheduledmessage.ErrClaimConflict
	}
	applyDelivery(row, claimed)
	row.LeaseToken, row.LeaseExpiresAt = nil, nil
	if current := r.schedules[schedule.ID]; current.Status != schedule.Status || current.NextScheduledAt != nil {
		r.schedules[schedule.ID] = cloneSchedule(schedule)
	}
	r.events = append(r.events, "retry")
	hook := r.afterRetry
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (r *memoryScheduledRepository) FinalizeOccurrence(_ context.Context, claimed *domainscheduledmessage.Delivery, schedule *domainscheduledmessage.Schedule, next *domainscheduledmessage.Delivery) error {
	r.mu.Lock()
	row := r.deliveries[claimed.ID]
	if !liveClaim(row, pointerStringValue(claimed.LeaseToken), claimed.UpdatedAt) {
		r.mu.Unlock()
		return interfacescheduledmessage.ErrClaimConflict
	}
	applyDelivery(row, claimed)
	row.LeaseToken, row.LeaseExpiresAt = nil, nil
	r.schedules[schedule.ID] = cloneSchedule(schedule)
	if next != nil {
		r.next[schedule.ID] = cloneDelivery(next)
	}
	r.events = append(r.events, "finalize")
	hook := r.afterFinalize
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (r *memoryScheduledRepository) DispatchAndFinalize(ctx context.Context, deliveryID, token string, now time.Time, nextFactory interfacescheduledmessage.NextOccurrenceFactory, dispatch interfacescheduledmessage.DispatchCallback) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery := r.deliveries[deliveryID]
	if !liveClaim(delivery, token, now) {
		return interfacescheduledmessage.ErrClaimConflict
	}
	schedule := r.schedules[delivery.ScheduledMessageID]
	if schedule.Status != domainscheduledmessage.ScheduleStatusActive {
		return interfacescheduledmessage.ErrClaimConflict
	}
	if r.inactiveSpaces[schedule.SpaceID] {
		return interfacescheduledmessage.ErrInvalidTarget
	}
	if err := r.targetErr[deliveryID]; err != nil {
		return err
	}
	identity := &domainidentity.ExternalIdentity{Provider: domainidentity.ProviderHermes, ExternalID: "15551234567@s.whatsapp.net", Status: domainidentity.StatusActive}
	finalizedAt, err := dispatch(ctx, cloneSchedule(schedule), cloneDelivery(delivery), nil, identity)
	if err != nil {
		return err
	}
	var next *domainscheduledmessage.Delivery
	if nextFactory != nil {
		next, err = nextFactory(cloneSchedule(schedule), finalizedAt)
		if err != nil {
			return fmt.Errorf("%w: next occurrence calculation failed", interfacescheduledmessage.ErrDispatchOutcomeUncertain)
		}
	}
	if r.uncertainAfter {
		return interfacescheduledmessage.ErrDispatchOutcomeUncertain
	}
	delivery.Status, delivery.SentAt, delivery.NextAttemptAt = domainscheduledmessage.DeliveryStatusSent, &finalizedAt, nil
	delivery.LeaseToken, delivery.LeaseExpiresAt = nil, nil
	schedule.UpdatedAt = finalizedAt
	if next == nil {
		schedule.Status, schedule.NextScheduledAt = domainscheduledmessage.ScheduleStatusSent, nil
	} else {
		schedule.NextScheduledAt = &next.ScheduledAt
		r.next[schedule.ID] = cloneDelivery(next)
	}
	r.events = append(r.events, "dispatch_finalize")
	return nil
}

func (r *memoryScheduledRepository) targetError(deliveryID string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targetErr[deliveryID] = err
}

func (r *memoryScheduledRepository) setSpaceActive(spaceID string, active bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if active {
		delete(r.inactiveSpaces, spaceID)
		return
	}
	r.inactiveSpaces[spaceID] = true
}

func liveClaim(delivery *domainscheduledmessage.Delivery, token string, now time.Time) bool {
	return delivery != nil && delivery.Status == domainscheduledmessage.DeliveryStatusPending && delivery.LeaseToken != nil && *delivery.LeaseToken == token && delivery.LeaseExpiresAt != nil && delivery.LeaseExpiresAt.After(now)
}

func applyDelivery(target, source *domainscheduledmessage.Delivery) {
	target.ContentSnapshot, target.GenerationAttempts, target.DeliveryAttempts = source.ContentSnapshot, source.GenerationAttempts, source.DeliveryAttempts
	target.NextAttemptAt, target.Status, target.FailureCategory, target.SentAt = source.NextAttemptAt, source.Status, source.FailureCategory, source.SentAt
	target.UpdatedAt = source.UpdatedAt
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) time() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type testGenerator struct {
	mu        sync.Mutex
	outputs   []string
	errors    []error
	inputs    []dto.ScheduledMessageGenerationInput
	afterCall func()
	gate      <-chan struct{}
	started   chan<- struct{}
}

func (g *testGenerator) Generate(ctx context.Context, input dto.ScheduledMessageGenerationInput) (string, error) {
	g.mu.Lock()
	g.inputs = append(g.inputs, input)
	index := len(g.inputs) - 1
	var output string
	var err error
	if index < len(g.outputs) {
		output = g.outputs[index]
	}
	if index < len(g.errors) {
		err = g.errors[index]
	}
	after, gate, started := g.afterCall, g.gate, g.started
	g.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if after != nil {
		after()
	}
	return output, err
}

func (g *testGenerator) calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.inputs)
}

type testSender struct {
	mu       sync.Mutex
	failures []error
	messages []string
	targets  []domainreminder.DeliveryTarget
	onSend   func()
}

func (s *testSender) Send(_ context.Context, target domainreminder.DeliveryTarget, message string) error {
	s.mu.Lock()
	s.targets = append(s.targets, target)
	s.messages = append(s.messages, message)
	index := len(s.messages) - 1
	var err error
	if index < len(s.failures) {
		err = s.failures[index]
	}
	onSend := s.onSend
	s.mu.Unlock()
	if onSend != nil {
		onSend()
	}
	return err
}

func (s *testSender) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

type testDueIndex struct {
	mu            sync.Mutex
	version       uint64
	inFlight      int
	degraded      bool
	ids           map[string]time.Time
	events        []string
	markCalls     int
	allErr        error
	upsertManyErr error
	removeManyErr error
	beforeMark    func()
}

func newTestDueIndex() *testDueIndex {
	return &testDueIndex{version: 1, degraded: true, ids: make(map[string]time.Time)}
}

func (i *testDueIndex) Upsert(_ context.Context, id string, at time.Time) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.version++
	i.inFlight++
	i.ids[id] = at
	i.inFlight--
	i.version++
	i.events = append(i.events, "upsert")
	return nil
}

func (i *testDueIndex) Remove(_ context.Context, id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.version++
	i.inFlight++
	delete(i.ids, id)
	i.inFlight--
	i.version++
	i.events = append(i.events, "remove")
	return nil
}

func (i *testDueIndex) BeginLifecycleMutation() {
	i.mu.Lock()
	i.version++
	i.inFlight++
	i.mu.Unlock()
}

func (i *testDueIndex) FinishLifecycleMutation(err error) {
	i.mu.Lock()
	i.version++
	i.inFlight--
	if err != nil {
		i.degraded = true
	}
	i.mu.Unlock()
}

func (i *testDueIndex) ReconcileVersion() uint64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.events = append(i.events, "version")
	return i.version
}

func (i *testDueIndex) MarkReconciled(version uint64) {
	if i.beforeMark != nil {
		i.beforeMark()
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.markCalls++
	i.events = append(i.events, "mark")
	if i.version == version && i.inFlight == 0 {
		i.degraded = false
	} else {
		i.degraded = true
	}
}

func (i *testDueIndex) HasDue(_ context.Context, now time.Time) (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.degraded {
		return false, errors.New("index dirty")
	}
	for _, at := range i.ids {
		if !at.After(now) {
			return true, nil
		}
	}
	return false, nil
}

func (i *testDueIndex) AllIDs(context.Context) ([]string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.events = append(i.events, "all_ids")
	if i.allErr != nil {
		return nil, i.allErr
	}
	ids := make([]string, 0, len(i.ids))
	for id := range i.ids {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (i *testDueIndex) UpsertMany(_ context.Context, rows []domainscheduledmessage.Delivery) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.events = append(i.events, "upsert_many")
	if i.upsertManyErr != nil {
		return i.upsertManyErr
	}
	for _, row := range rows {
		if row.NextAttemptAt != nil {
			i.ids[row.ID] = *row.NextAttemptAt
		}
	}
	return nil
}

func (i *testDueIndex) RemoveMany(_ context.Context, ids []string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.events = append(i.events, "remove_many")
	if i.removeManyErr != nil {
		return i.removeManyErr
	}
	for _, id := range ids {
		delete(i.ids, id)
	}
	return nil
}

func orderedSubsequence(values, want []string) bool {
	position := 0
	for _, value := range values {
		if position < len(want) && value == want[position] {
			position++
		}
	}
	return position == len(want)
}

func cloneSchedule(schedule *domainscheduledmessage.Schedule) *domainscheduledmessage.Schedule {
	if schedule == nil {
		return nil
	}
	copy := *schedule
	copy.RecipientMemberID = cloneString(schedule.RecipientMemberID)
	copy.SpaceChannelID = cloneString(schedule.SpaceChannelID)
	copy.NextScheduledAt = cloneTime(schedule.NextScheduledAt)
	return &copy
}

func cloneDelivery(delivery *domainscheduledmessage.Delivery) *domainscheduledmessage.Delivery {
	if delivery == nil {
		return nil
	}
	copy := *delivery
	copy.ContentSnapshot = cloneString(delivery.ContentSnapshot)
	copy.NextAttemptAt = cloneTime(delivery.NextAttemptAt)
	copy.LeaseToken = cloneString(delivery.LeaseToken)
	copy.LeaseExpiresAt = cloneTime(delivery.LeaseExpiresAt)
	copy.FailureCategory = cloneString(delivery.FailureCategory)
	copy.SentAt = cloneTime(delivery.SentAt)
	return &copy
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func schedTestTimePointer(value time.Time) *time.Time { return &value }

func schedTestStringPointer(value string) *string { return &value }

var _ interfacescheduledmessage.SchedulerRepository = (*memoryScheduledRepository)(nil)
var _ interfacescheduledmessage.DueIndex = (*testDueIndex)(nil)
var _ interfacescheduledmessage.Generator = (*testGenerator)(nil)
var _ interfacenotification.NotificationSender = (*testSender)(nil)
