package interfaceactivity

import (
	"context"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
)

type RepoActivityInterface interface {
	Create(context.Context, *domainactivity.Activity) error
	List(context.Context, domainactivity.ListFilter) ([]domainactivity.Activity, error)
	FindByIDInSpace(context.Context, string, string) (*domainactivity.Activity, error)
	Update(context.Context, string, string, domainactivity.UpdateFields, time.Time) error
	// UpdateLocked locks the row, builds the update from it via prepare and
	// writes it in one transaction, returning the stored result.
	UpdateLocked(ctx context.Context, spaceID, activityID string, prepare func(*domainactivity.Activity) (domainactivity.UpdateFields, error), updatedAt time.Time) (*domainactivity.Activity, error)
	SoftDelete(context.Context, string, string, time.Time) error
	ReportFeeding(context.Context, domainactivity.FeedingReportRequest) (*domainactivity.FeedingReportResult, error)
}
