package main

import (
	"context"
	"net"
	"testing"
	"time"

	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	"family-assistant/pkg/config"
)

func TestFailOnErrorWithNilError(t *testing.T) {
	FailOnError(nil, "should not fail")
}

func TestFindIPv4AddressSkipsLoopbackAndIPv6(t *testing.T) {
	addresses := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(32, 32)},
		&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(128, 128)},
		&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(32, 32)},
	}

	if got := findIPv4Address(addresses); got != "192.0.2.10" {
		t.Fatalf("findIPv4Address() = %q, want %q", got, "192.0.2.10")
	}
}

func TestFindIPv4AddressReturnsUnknownWithoutUsableAddress(t *testing.T) {
	if got := findIPv4Address([]net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(32, 32)},
	}); got != "unknown" {
		t.Fatalf("findIPv4Address() = %q, want %q", got, "unknown")
	}
}

func TestStartScheduledMessageLaneSkipsDisabledConfig(t *testing.T) {
	service, err := startScheduledMessageLane(context.Background(), config.ScheduledMessageConfig{}, scheduledMessageLaneDependencies{})
	if err != nil || service != nil {
		t.Fatalf("disabled lane returned service/error = %v/%v, want nil/nil", service, err)
	}
}

func TestNewGroupBinderNeedsNoScheduledMessageLane(t *testing.T) {
	if binder := newGroupBinder(scheduledMessageLaneDependencies{}); binder == nil {
		t.Fatal("newGroupBinder() = nil, want a binder available while the scheduled message lane is disabled")
	}
}

func TestScheduledMessageGeneratorIsOptionalAndConstructedWhenConfigured(t *testing.T) {
	generator, err := newScheduledMessageGenerator(config.ScheduledMessageConfig{})
	if err != nil || generator != nil {
		t.Fatalf("unset optional generator = %v/%v, want nil/nil", generator, err)
	}

	generator, err = newScheduledMessageGenerator(config.ScheduledMessageConfig{
		GeneratorURL: "http://127.0.0.1:8000/v1/chat/completions", GeneratorKey: "test-key", GeneratorModel: "test-model",
		GeneratorTimeout: time.Second, MaxTextBytes: 2000, MaxOutputBytes: 2000, MaxResponseBytes: 1024,
	})
	if err != nil || generator == nil {
		t.Fatalf("configured optional generator = %v/%v, want a generator", generator, err)
	}
}

func TestScheduledMessageWorkerRunsIndependentlyAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runner := &scheduledMessageRunnerStub{started: make(chan context.Context, 1), stopped: make(chan struct{})}
	startDone := make(chan error, 1)
	go func() {
		startDone <- startScheduledMessageWorker(ctx, config.ScheduledMessageConfig{Enabled: true}, runner)
	}()
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("start worker: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker startup blocked on scheduler work")
	}
	select {
	case workerContext := <-runner.started:
		if workerContext != ctx {
			t.Fatal("worker did not receive the application shutdown context")
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	reminderTick := make(chan struct{})
	go func() { close(reminderTick) }()
	select {
	case <-reminderTick:
	case <-time.After(time.Second):
		t.Fatal("a blocked scheduled-message worker stalled the reminder lane")
	}

	cancel()
	select {
	case <-runner.stopped:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after application context cancellation")
	}
}

type scheduledMessageRunnerStub struct {
	started chan context.Context
	stopped chan struct{}
}

func (s *scheduledMessageRunnerStub) Run(ctx context.Context) {
	s.started <- ctx
	<-ctx.Done()
	close(s.stopped)
}

func (*scheduledMessageRunnerStub) RunOnce(context.Context, time.Time) error { return nil }

var _ interfacescheduledmessage.Scheduler = (*scheduledMessageRunnerStub)(nil)
