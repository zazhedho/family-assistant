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

// ReportFeedingLeftover attaches a leftover or outcome report to one feeding.
// The repository picks the feeding; a *domainactivity.FeedingTargetError is
// returned unwrapped so callers can show the candidates.
func (s *service) ReportFeedingLeftover(ctx context.Context, actor domainidentity.ActorContext, input dto.FeedingLeftoverInput) (result *domainactivity.FeedingReportResult, err error) {
	spaceID := strings.TrimSpace(input.Space)
	if spaceID == "" {
		spaceID = strings.TrimSpace(actor.SpaceID)
	}
	resourceID := strings.TrimSpace(input.FeedingID)
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
