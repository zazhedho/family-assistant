package interfacescheduledmessage

import (
	"context"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
)

type Repository interface {
	Create(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery) error
	FindByIDInSpace(context.Context, string, string) (*domainscheduledmessage.Schedule, error)
	List(context.Context, dto.ScheduledMessageListInput) ([]domainscheduledmessage.Schedule, error)
	ListHistory(context.Context, string, string, int) ([]domainscheduledmessage.Delivery, error)
	// UpdateActive returns UUIDs of pending delivery rows invalidated or replaced by this update.
	UpdateActive(context.Context, string, string, dto.ScheduledMessageUpdateInput, *domainscheduledmessage.Delivery, time.Time) ([]string, error)
	// Cancel returns UUIDs of pending delivery rows atomically cancelled with the schedule.
	Cancel(context.Context, string, string, time.Time) ([]string, error)
	// Unsubscribe returns UUIDs of pending delivery rows atomically cancelled with the schedule.
	Unsubscribe(context.Context, string, string, string, time.Time) ([]string, error)
	BindCurrentGroup(context.Context, string, string, string, string, string, time.Time) (*domainscheduledmessage.SpaceChannel, error)
	ListTargets(context.Context, string) ([]dto.ScheduledMessageTargetOutput, error)
}

type SchedulerRepository interface {
	ListGeneratedHistory(context.Context, string, string, int) ([]string, error)
	ListPendingForIndex(context.Context) ([]domainscheduledmessage.Delivery, error)
	ClaimDue(context.Context, time.Time, time.Duration, int) ([]domainscheduledmessage.Delivery, error)
	LoadClaimed(context.Context, string, string, time.Time) (*domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity, string, error)
	ReserveGenerationAttempt(context.Context, *domainscheduledmessage.Delivery, time.Time, int) error
	ReserveDeliveryAttempt(context.Context, *domainscheduledmessage.Delivery, time.Time, int) error
	PersistGeneratedContent(context.Context, *domainscheduledmessage.Delivery, string, time.Time) error
	RecordRetry(context.Context, *domainscheduledmessage.Delivery, *domainscheduledmessage.Schedule) error
	FinalizeOccurrence(context.Context, *domainscheduledmessage.Delivery, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery) error
	DispatchAndFinalize(context.Context, string, string, time.Time, NextOccurrenceFactory, DispatchCallback) error
}

type NextOccurrenceFactory func(*domainscheduledmessage.Schedule, time.Time) (*domainscheduledmessage.Delivery, error)

type DispatchCallback func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error)
