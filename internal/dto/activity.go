package dto

import "time"

type ActivityCreateInput struct {
	Space      string
	Kind       string
	Subject    string
	Note       string
	Details    map[string]any
	OccurredAt time.Time
	// MessageAt is the trusted time of the request message. It stamps a
	// feeding report sent with the create; zero falls back to OccurredAt.
	MessageAt time.Time
}

type ActivityListInput struct {
	Space  string
	Kind   string
	Search string
	From   *time.Time
	To     *time.Time
	Limit  int
}

type ActivityUpdateInput struct {
	Kind       *string
	Subject    *string
	Note       *string
	Details    map[string]any
	OccurredAt *time.Time
}

// FeedingLeftoverInput reports a leftover or outcome for one feeding.
// MessageAt is when the report was sent; FeedingID or FeedingTime pick the
// feeding, otherwise the open feedings before MessageAt are considered.
type FeedingLeftoverInput struct {
	Space       string
	FeedingID   string
	FeedingTime *time.Time
	MessageAt   time.Time
	LeftoverML  *int
	ExpiredML   *int
	Outcome     string
}
