package serviceactivity

import (
	"context"
	"errors"
	"strings"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainaudit "family-assistant/internal/domain/audit"
	domainauthorization "family-assistant/internal/domain/authorization"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
	"gorm.io/gorm"
)

// maxFeedingReportAge is how far before the message a named feeding time may be.
const maxFeedingReportAge = 48 * time.Hour

func init() { registerKindHooks("feeding", feedingCreate, feedingUpdate) }

// feedingCreate: a feeding with volume_ml gets a generated note; its
// leftover_ml/outcome/expired_ml go through the feeding report rules and are
// stamped with the message time.
func feedingCreate(subject string, details domainactivity.Details, messageAt time.Time, validate func() error) (domainactivity.Details, string, error) {
	if _, ok := details["leftover_reported_at"]; ok {
		return nil, "", &serviceauthorization.ValidationError{Field: "details.leftover_reported_at", Reason: "is set by the server"}
	}
	if err := validate(); err != nil {
		return nil, "", err
	}
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
	return details, "", nil
}

// feedingUpdate guards the report keys and, when the caller sent no note,
// regenerates the note only if the stored one was backend-generated. A note a
// person wrote (legacy prose feedings included) is never replaced.
func feedingUpdate(stored string, activity *domainactivity.Activity, fields *domainactivity.UpdateFields, merged domainactivity.Details, validate func() error) error {
	storedFeeding := stored == "feeding"
	if storedFeeding {
		for _, key := range domainactivity.FeedingReportedKeys() {
			if _, ok := fields.DetailsPatch[key]; ok {
				return &serviceauthorization.ValidationError{Field: "details", Reason: key + " can only be set by the feeding leftover report"}
			}
		}
	}
	// A record turned into a feeding must not carry a report it never went
	// through the report tool for; the caller may remove those keys with null.
	if !storedFeeding {
		for _, key := range domainactivity.FeedingReportedKeys() {
			if _, ok := merged[key]; ok {
				return &serviceauthorization.ValidationError{Field: "details", Reason: key + " can only be set by the feeding leftover report; remove it when changing the kind to feeding"}
			}
		}
	}
	if err := validate(); err != nil {
		return err
	}
	if fields.Note == nil && domainactivity.IsGeneratedFeedingNote(activity.Subject, activity.Details, activity.Note) {
		subject := activity.Subject
		if fields.Subject != nil {
			subject = *fields.Subject
		}
		note := domainactivity.FeedingNote(subject, merged)
		fields.Note = &note
	}
	return nil
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

// ReportFeedingLeftover attaches a leftover or outcome report to one feeding.
// The repository picks the feeding; a *domainactivity.FeedingTargetError is
// returned unwrapped so callers can show the candidates.
func (s *service) ReportFeedingLeftover(ctx context.Context, actor domainidentity.ActorContext, input dto.FeedingLeftoverInput) (result *domainactivity.FeedingReportResult, err error) {
	spaceID := strings.TrimSpace(input.Space)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	// The audit resource id is set only once the feeding id is a valid UUID.
	resourceID := ""
	defer func() {
		if err == nil {
			return
		}
		event := activityAuditEvent(actor, domainaudit.ActionUpdate, spaceID, resourceID, domainaudit.StatusFailed)
		event.ErrorMessage = failureCategory(err)
		s.writeAudit(ctx, event)
	}()

	if s.authorize == nil || s.spaces == nil || s.activities == nil {
		return nil, errors.New("activity service is not configured")
	}
	selector, err := feedingSelector(input)
	if err != nil {
		return nil, err
	}
	resourceID = selector.FeedingID
	if err := s.authorize.Authorize(ctx, actor, updatePermission, domainauthorization.Resource{SpaceID: spaceID}); err != nil {
		return nil, err
	}
	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	actorMember := activityMember(members, actor.MemberID, actor.UserID)
	if actorMember == nil {
		return nil, serviceauthorization.ErrNotFound
	}
	role, memberID := actorMember.RoleName, actorMember.ID
	result, err = s.activities.ReportFeeding(ctx, domainactivity.FeedingReportRequest{
		SpaceID:  spaceID,
		Selector: selector,
		Report: domainactivity.FeedingReport{
			LeftoverML: input.LeftoverML, ExpiredML: input.ExpiredML, Outcome: strings.ToLower(strings.TrimSpace(input.Outcome)),
		},
		ReportedAt: selector.MessageAt,
		Now:        time.Now().UTC(),
		CanMutate:  func(activity *domainactivity.Activity) bool { return canMutateActivity(role, memberID, activity) },
	})
	if err != nil {
		return nil, feedingReportError(err)
	}
	if result == nil {
		return nil, errors.New("feeding report returned no result")
	}
	resourceID = result.Activity.ID
	event := activityAuditEvent(actor, domainaudit.ActionUpdate, spaceID, resourceID, domainaudit.StatusSuccess)
	event.Metadata["previous_details"] = result.Previous
	event.Metadata["changed"] = result.Changed
	s.writeAudit(ctx, event)
	return result, nil
}

func feedingSelector(input dto.FeedingLeftoverInput) (domainactivity.FeedingSelector, error) {
	var selector domainactivity.FeedingSelector
	if input.MessageAt.IsZero() {
		return selector, &serviceauthorization.ValidationError{Field: "message_at", Reason: requiredReason}
	}
	selector.MessageAt = input.MessageAt.UTC()
	if raw := strings.TrimSpace(input.FeedingID); raw != "" {
		id, err := utils.ParseUUID(raw, "feeding_id", true)
		if err != nil || len(id) != 36 {
			return selector, &serviceauthorization.ValidationError{Field: "feeding_id", Reason: "must be a UUID"}
		}
		selector.FeedingID = strings.ToLower(id)
	}
	if input.FeedingTime != nil {
		feedingTime := input.FeedingTime.UTC()
		if feedingTime.Before(selector.MessageAt.Add(-maxFeedingReportAge)) || feedingTime.After(selector.MessageAt.Add(domainactivity.FeedingTimeTolerance)) {
			return selector, &serviceauthorization.ValidationError{Field: "feeding_time", Reason: "must be within 48 hours before the message and not after it"}
		}
		selector.FeedingTime = &feedingTime
	}
	return selector, nil
}

func feedingReportError(err error) error {
	var targetErr *domainactivity.FeedingTargetError
	if errors.As(err, &targetErr) {
		return targetErr
	}
	var detailsErr *domainactivity.DetailsError
	switch {
	case errors.As(err, &detailsErr):
		return &serviceauthorization.ValidationError{Field: detailsErr.Field, Reason: detailsErr.Reason}
	case errors.Is(err, domainactivity.ErrFeedingForbidden):
		return serviceauthorization.ErrForbidden
	case errors.Is(err, gorm.ErrRecordNotFound):
		return serviceauthorization.ErrNotFound
	}
	return err
}
