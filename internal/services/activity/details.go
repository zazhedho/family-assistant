package serviceactivity

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	domainactivity "family-assistant/internal/domain/activity"
	serviceauthorization "family-assistant/internal/services/authorization"
)

const (
	feedingKind      = "feeding"
	maxSubjectLength = 128
)

func normalizeSubject(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if utf8.RuneCountInString(subject) > maxSubjectLength {
		return "", &serviceauthorization.ValidationError{Field: "subject", Reason: "must be at most 128 characters"}
	}
	return subject, nil
}

// detailsValidationError converts a domain details error into a service
// validation error. prefix is prepended to fields that are not already
// details paths, so report errors raised on create name the details key.
func detailsValidationError(err error, prefix string) error {
	var detailsErr *domainactivity.DetailsError
	if !errors.As(err, &detailsErr) {
		return err
	}
	field := detailsErr.Field
	if prefix != "" && field != "details" && !strings.HasPrefix(field, "details.") {
		if field == "report" {
			field = "details"
		} else {
			field = prefix + field
		}
	}
	return &serviceauthorization.ValidationError{Field: field, Reason: detailsErr.Reason}
}

// createDetails validates the details of a new activity and returns them with
// the note to store. A feeding with volume_ml gets a generated note; its
// leftover_ml/outcome/expired_ml go through the feeding report rules and are
// stamped with the message time.
func createDetails(kind, subject string, raw map[string]any, note string, messageAt time.Time) (domainactivity.Details, string, error) {
	details := domainactivity.ApplyDetailsPatch(nil, domainactivity.Details(raw))
	if kind == feedingKind {
		if _, ok := details["leftover_reported_at"]; ok {
			return nil, "", &serviceauthorization.ValidationError{Field: "details.leftover_reported_at", Reason: "is set by the server"}
		}
	}
	if err := domainactivity.ValidateDetails(kind, details); err != nil {
		return nil, "", detailsValidationError(err, "")
	}
	if kind == feedingKind {
		base, report, hasReport := splitFeedingReport(details)
		_, hasVolume := details["volume_ml"]
		switch {
		case hasVolume && hasReport:
			next, _, err := domainactivity.ApplyFeedingReport(base, report, messageAt)
			if err != nil {
				return nil, "", detailsValidationError(err, "details.")
			}
			return next, domainactivity.FeedingNote(subject, next), nil
		case hasVolume:
			return details, domainactivity.FeedingNote(subject, details), nil
		case hasReport:
			return nil, "", &serviceauthorization.ValidationError{Field: "details.volume_ml", Reason: "is required when reporting leftover_ml, outcome or expired_ml"}
		}
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, "", &serviceauthorization.ValidationError{Field: "note", Reason: requiredReason}
	}
	return details, note, nil
}

// splitFeedingReport separates the reported keys of validated feeding details
// into a FeedingReport, returning the remaining details as the base.
func splitFeedingReport(details domainactivity.Details) (domainactivity.Details, domainactivity.FeedingReport, bool) {
	base := domainactivity.ApplyDetailsPatch(details, nil)
	var report domainactivity.FeedingReport
	found := false
	if v, ok := details["leftover_ml"]; ok {
		n, _ := intValue(v)
		report.LeftoverML = &n
		found = true
	}
	if v, ok := details["expired_ml"]; ok {
		n, _ := intValue(v)
		report.ExpiredML = &n
		found = true
	}
	if v, ok := details["outcome"]; ok {
		report.Outcome, _ = v.(string)
		found = true
	}
	for _, key := range domainactivity.FeedingReportedKeys() {
		delete(base, key)
	}
	return base, report, found
}

// intValue reads a whole number decoded from JSON or built in Go. Callers run
// it on details already accepted by ValidateDetails.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), n == float64(int(n))
	case float32:
		return int(n), n == float32(int(n))
	case interface{ Int64() (int64, error) }:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// prepareDetailsUpdate validates an update against the merged details and the
// resulting kind, and regenerates a feeding note when the caller sent none.
// It returns the merged details.
func prepareDetailsUpdate(activity *domainactivity.Activity, fields *domainactivity.UpdateFields) (domainactivity.Details, error) {
	kind := domainactivity.NormalizeKind(activity.Kind)
	if fields.Kind != nil {
		kind = *fields.Kind
	}
	if kind == feedingKind {
		for _, key := range domainactivity.FeedingReportedKeys() {
			if _, ok := fields.DetailsPatch[key]; ok {
				return nil, &serviceauthorization.ValidationError{Field: "details", Reason: key + " can only be set by the feeding leftover report"}
			}
		}
	}
	merged := domainactivity.ApplyDetailsPatch(activity.Details, fields.DetailsPatch)
	if fields.Kind != nil || len(fields.DetailsPatch) > 0 {
		if err := domainactivity.ValidateDetails(kind, merged); err != nil {
			return nil, detailsValidationError(err, "")
		}
	}
	if _, hasVolume := merged["volume_ml"]; kind == feedingKind && hasVolume && fields.Note == nil {
		subject := activity.Subject
		if fields.Subject != nil {
			subject = *fields.Subject
		}
		note := domainactivity.FeedingNote(subject, merged)
		fields.Note = &note
	}
	return merged, nil
}
