package main

import (
	"context"
	"testing"

	domainspace "family-assistant/internal/domain/space"
	interfacefinance "family-assistant/internal/interfaces/finance"
	"family-assistant/pkg/config"
)

type financeRepoStub struct {
	interfacefinance.RepoFinanceInterface
}

type financeMembersStub struct{}

func (financeMembersStub) ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
}

func TestStartFinanceSchedulerSkipsDisabledConfig(t *testing.T) {
	scheduler, err := startFinanceScheduler(context.Background(), config.FinanceRecurringConfig{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("start disabled scheduler: %v", err)
	}
	if scheduler != nil {
		t.Fatal("disabled scheduler should not start")
	}
}

func TestStartFinanceSchedulerRequiresDependenciesWhenEnabled(t *testing.T) {
	if _, err := startFinanceScheduler(context.Background(), config.FinanceRecurringConfig{Enabled: true}, nil, nil, nil); err == nil {
		t.Fatal("enabled scheduler without a repository must fail to start")
	}
}

func TestStartFinanceSchedulerBuildsEnabledSchedulerAndHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler, err := startFinanceScheduler(ctx, config.FinanceRecurringConfig{Enabled: true}, financeRepoStub{}, financeMembersStub{}, nil)
	if err != nil {
		t.Fatalf("start enabled scheduler: %v", err)
	}
	if scheduler == nil {
		t.Fatal("enabled scheduler was not created")
	}
}
