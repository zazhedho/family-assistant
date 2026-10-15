package interfacescheduledmessage

import (
	"context"
	"time"

	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
)

// DueIndexWriter keys every entry by immutable delivery UUID, never schedule UUID.
type DueIndexWriter interface {
	Upsert(context.Context, string, time.Time) error
	Remove(context.Context, string) error
	BeginLifecycleMutation()
	FinishLifecycleMutation(error)
}

type DueIndex interface {
	DueIndexWriter
	ReconcileVersion() uint64
	MarkReconciled(uint64)
	HasDue(context.Context, time.Time) (bool, error)
	AllIDs(context.Context) ([]string, error)
	UpsertMany(context.Context, []domainscheduledmessage.Delivery) error
	RemoveMany(context.Context, []string) error
}
