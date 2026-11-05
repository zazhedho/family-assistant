package domainactivity

import (
	"errors"
	"time"
)

// ErrFeedingForbidden means the caller may not change the selected feeding.
var ErrFeedingForbidden = errors.New("feeding report forbidden")

// FeedingReportRequest asks the repository to attach a report to one feeding.
// CanMutate, when set, is checked against the selected feeding before any write.
type FeedingReportRequest struct {
	SpaceID         string
	Selector        FeedingSelector
	Report          FeedingReport
	ReportedAt, Now time.Time
	CanMutate       func(*Activity) bool
}

// FeedingReportResult is the feeding after the report and its details before it.
type FeedingReportResult struct {
	Activity Activity
	Previous Details
	Changed  bool
}
