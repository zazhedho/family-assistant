package domainactivity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// FeedingTimeTolerance is how far a reported feeding time may differ from a stored one.
	FeedingTimeTolerance = 20 * time.Minute
	// FeedingOpenLookback is how far before the message an open feeding is still a default target.
	FeedingOpenLookback = 3 * time.Hour

	maxFeedingCandidates = 5
)

type FeedingSelector struct {
	FeedingID   string
	FeedingTime *time.Time
	MessageAt   time.Time
}

type FeedingReport struct {
	LeftoverML, ExpiredML *int
	Outcome               string
}

type FeedingCandidate struct {
	ID         string
	OccurredAt time.Time
	VolumeML   *int
	Open       bool
}

// FeedingTargetError reports that no single feeding could be chosen.
type FeedingTargetError struct {
	Reason     string // "ambiguous" or "none"
	Candidates []FeedingCandidate
}

func (e *FeedingTargetError) Error() string {
	return "feeding target " + e.Reason
}

// FeedingReportedKeys are the details keys set only through a feeding report.
func FeedingReportedKeys() []string {
	return []string{"leftover_ml", "leftover_reported_at", "outcome", "expired_ml"}
}

func isOpenFeeding(a Activity) bool {
	s, _ := a.Details["outcome"].(string)
	return s == ""
}

func candidateOf(a Activity) FeedingCandidate {
	c := FeedingCandidate{ID: a.ID, OccurredAt: a.OccurredAt, Open: isOpenFeeding(a)}
	if v, ok := toInt(a.Details["volume_ml"]); ok {
		c.VolumeML = &v
	}
	return c
}

func candidates(list []Activity) []FeedingCandidate {
	out := make([]FeedingCandidate, 0, len(list))
	for _, a := range list {
		out = append(out, candidateOf(a))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.Before(out[j].OccurredAt) })
	return out
}

func recent(feedings []Activity) []FeedingCandidate {
	out := candidates(feedings)
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	if len(out) > maxFeedingCandidates {
		out = out[:maxFeedingCandidates]
	}
	return out
}

func pick(matches []Activity, feedings []Activity) (*Activity, error) {
	switch len(matches) {
	case 0:
		return nil, &FeedingTargetError{Reason: "none", Candidates: recent(feedings)}
	case 1:
		m := matches[0]
		return &m, nil
	}
	return nil, &FeedingTargetError{Reason: "ambiguous", Candidates: candidates(matches)}
}

// SelectFeeding picks the feeding a report refers to: by ID, else by time
// within tolerance, else the open feedings in the lookback window.
func SelectFeeding(feedings []Activity, sel FeedingSelector) (*Activity, error) {
	if sel.FeedingID != "" {
		for i := range feedings {
			if feedings[i].ID == sel.FeedingID {
				a := feedings[i]
				return &a, nil
			}
		}
		return nil, &FeedingTargetError{Reason: "none", Candidates: recent(feedings)}
	}
	var matches []Activity
	if sel.FeedingTime != nil {
		for _, a := range feedings {
			d := a.OccurredAt.Sub(*sel.FeedingTime)
			if d < 0 {
				d = -d
			}
			if d <= FeedingTimeTolerance {
				matches = append(matches, a)
			}
		}
		return pick(matches, feedings)
	}
	for _, a := range feedings {
		if !isOpenFeeding(a) || a.OccurredAt.After(sel.MessageAt) || sel.MessageAt.Sub(a.OccurredAt) > FeedingOpenLookback {
			continue
		}
		matches = append(matches, a)
	}
	return pick(matches, feedings)
}

// ApplyFeedingReport merges a report into feeding details. A leftover-only
// report reopens the feeding (removes outcome and expired_ml). It returns
// changed=false (and the unchanged details) when the report matches the
// stored state.
func ApplyFeedingReport(current Details, report FeedingReport, reportedAt time.Time) (Details, bool, error) {
	if report.LeftoverML == nil && report.Outcome == "" {
		return nil, false, detailsErr("report", "leftover_ml or outcome is required")
	}
	if report.Outcome != "" && report.Outcome != OutcomeFinished && report.Outcome != OutcomeExpired {
		return nil, false, detailsErr("outcome", "must be finished or expired")
	}
	for name, p := range map[string]*int{"leftover_ml": report.LeftoverML, "expired_ml": report.ExpiredML} {
		if p != nil && *p < 0 {
			return nil, false, detailsErr(name, "must be >= 0")
		}
	}
	next := ApplyDetailsPatch(current, nil)
	if report.LeftoverML != nil {
		next["leftover_ml"] = *report.LeftoverML
	}
	switch report.Outcome {
	case "":
		// A leftover-only report reopens the feeding: corrections overwrite,
		// so an earlier outcome and its expired amount are cleared.
		if report.ExpiredML != nil {
			return nil, false, detailsErr("expired_ml", "requires outcome expired")
		}
		delete(next, "outcome")
		delete(next, "expired_ml")
	case OutcomeFinished:
		if report.ExpiredML != nil && *report.ExpiredML != 0 {
			return nil, false, detailsErr("expired_ml", "must be 0 when outcome is finished")
		}
		next["outcome"] = OutcomeFinished
		next["expired_ml"] = 0
	case OutcomeExpired:
		next["outcome"] = OutcomeExpired
		if report.ExpiredML != nil {
			next["expired_ml"] = *report.ExpiredML
		} else {
			stored, ok := toInt(current["expired_ml"])
			// A stored 0 belongs to a finished feeding, not to an expiry.
			if !ok || (stringOf(current["outcome"]) == OutcomeFinished && stored == 0) {
				return nil, false, detailsErr("expired_ml", "is required when outcome is expired")
			}
			next["expired_ml"] = stored
		}
	}

	if err := validateFeeding(next); err != nil {
		return nil, false, err
	}

	leftoverChanged := !sameInt(current["leftover_ml"], next["leftover_ml"])
	outcomeChanged := stringOf(current["outcome"]) != stringOf(next["outcome"])
	expiredChanged := !sameInt(current["expired_ml"], next["expired_ml"])
	if !leftoverChanged && !outcomeChanged && !expiredChanged {
		return ApplyDetailsPatch(current, nil), false, nil
	}
	if leftoverChanged || outcomeChanged {
		next["leftover_reported_at"] = reportedAt.Format(time.RFC3339)
	}
	return next, true, nil
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func sameInt(a, b any) bool {
	x, okA := toInt(a)
	y, okB := toInt(b)
	return okA == okB && x == y
}

// FeedingNote renders a deterministic Indonesian note from structured details.
// It never includes free text from the details.
func FeedingNote(subject string, details Details) string {
	head := strings.TrimSpace("Feeding " + strings.TrimSpace(subject))
	var parts []string
	if v, ok := toInt(details["volume_ml"]); ok {
		parts = append(parts, fmt.Sprintf("volume %d ml", v))
	}
	if v, ok := toInt(details["leftover_ml"]); ok {
		parts = append(parts, fmt.Sprintf("sisa %d ml", v))
	}
	switch stringOf(details["outcome"]) {
	case OutcomeFinished:
		parts = append(parts, "habis diminum")
	case OutcomeExpired:
		if v, ok := toInt(details["expired_ml"]); ok {
			parts = append(parts, fmt.Sprintf("expired %d ml", v))
		} else {
			parts = append(parts, "expired")
		}
	}
	if len(parts) == 0 {
		return head
	}
	return head + " — " + strings.Join(parts, "; ")
}

// IsGeneratedFeedingNote reports whether note is the one the backend would
// write for subject and details, i.e. no person wrote it. Evaluate it on the
// stored row before a change: only such notes may be regenerated.
func IsGeneratedFeedingNote(subject string, details Details, note string) bool {
	return note != "" && note == FeedingNote(subject, details)
}

// FeedingReportOverwrites reports whether a report changed or removed a
// reported value (leftover_ml, outcome, expired_ml) that existed before it.
// Adding a value for the first time, e.g. "finished" after a leftover, is a
// normal follow-up and not an overwrite.
func FeedingReportOverwrites(previous, next Details) bool {
	for _, key := range []string{"leftover_ml", "expired_ml"} {
		if _, had := previous[key]; had && !sameInt(previous[key], next[key]) {
			return true
		}
	}
	if _, had := previous["outcome"]; had && stringOf(previous["outcome"]) != stringOf(next["outcome"]) {
		return true
	}
	return false
}
