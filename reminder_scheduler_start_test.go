package main

import (
	"context"
	"testing"

	"family-assistant/pkg/config"
)

func TestStartReminderSchedulerSkipsDisabledConfig(t *testing.T) {
	scheduler, err := startReminderScheduler(context.Background(), config.ReminderSchedulerConfig{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("start disabled scheduler: %v", err)
	}
	if scheduler != nil {
		t.Fatal("disabled scheduler should not start")
	}
}

func TestStartReminderSchedulerBuildsEnabledSchedulerAndHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler, err := startReminderScheduler(ctx, config.ReminderSchedulerConfig{
		Enabled: true, WhatsAppBridgeURL: "http://127.0.0.1:8765",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("start enabled scheduler: %v", err)
	}
	if scheduler == nil {
		t.Fatal("enabled scheduler was not created")
	}
}
