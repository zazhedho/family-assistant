package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
)

type ActivityReminderInput struct {
	Title            string `json:"title"`
	Description      string `json:"description,omitempty"`
	ScheduledAt      string `json:"scheduled_at,omitempty" jsonschema:"absolute RFC3339 timestamp; provide exactly one of scheduled_at or after_minutes"`
	AfterMinutes     *int64 `json:"after_minutes,omitempty" jsonschema:"positive integer minutes after activity.occurred_at; provide exactly one of scheduled_at or after_minutes"`
	AssigneeMemberID string `json:"assignee_member_id,omitempty"`
}

type ActivityCreateWithReminderInput struct {
	Activity ActivityCreateInput   `json:"activity"`
	Reminder ActivityReminderInput `json:"reminder"`
}

type ActivityCreateWithReminderOutput struct {
	Status        string                   `json:"status"`
	Activity      *domainactivity.Activity `json:"activity"`
	Reminder      *ReminderOutput          `json:"reminder,omitempty"`
	ReminderError string                   `json:"reminder_error,omitempty"`
}

func ActivityCreateWithReminder(ctx context.Context, resolver interfaceidentity.Resolver, activityService interfaceactivity.ServiceActivityInterface, reminderService interfacereminder.ServiceReminderInterface, input ActivityCreateWithReminderInput) (ActivityCreateWithReminderOutput, error) {
	if reminderService == nil {
		return ActivityCreateWithReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	if strings.TrimSpace(input.Reminder.Title) == "" {
		return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "title", Reason: "is required"})
	}

	hasScheduledAt := strings.TrimSpace(input.Reminder.ScheduledAt) != ""
	hasAfterMinutes := input.Reminder.AfterMinutes != nil
	if hasScheduledAt == hasAfterMinutes {
		return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "schedule", Reason: "exactly one of scheduled_at or after_minutes is required"})
	}

	occurredAt, err := activityOccurredAt(ctx, input.Activity.OccurredAt)
	if err != nil {
		return ActivityCreateWithReminderOutput{}, MapToolError(err)
	}
	input.Activity.OccurredAt = occurredAt.Format(time.RFC3339Nano)
	var scheduledAt *time.Time
	if hasScheduledAt {
		var err error
		scheduledAt, err = utils.ParseRFC3339(input.Reminder.ScheduledAt, "scheduled_at", true)
		if err != nil {
			return ActivityCreateWithReminderOutput{}, MapToolError(err)
		}
	} else {
		afterMinutes := *input.Reminder.AfterMinutes
		if afterMinutes <= 0 {
			return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "after_minutes", Reason: "must be a positive integer"})
		}
		const maxAfterMinutes = (1<<63 - 1) / time.Minute
		if time.Duration(afterMinutes) > maxAfterMinutes {
			return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "after_minutes", Reason: "is too large"})
		}
		computed := occurredAt.Add(time.Duration(afterMinutes) * time.Minute)
		if computed.Year() > 9999 {
			return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "after_minutes", Reason: "produces a time outside the RFC3339 range"})
		}
		scheduledAt = &computed
	}

	assignee, err := utils.ParseOptionalUUID(input.Reminder.AssigneeMemberID, "assignee_member_id")
	if err != nil {
		return ActivityCreateWithReminderOutput{}, MapToolError(err)
	}

	activity, err := ActivityCreate(ctx, resolver, activityService, input.Activity)
	if err != nil {
		return ActivityCreateWithReminderOutput{}, err
	}
	if activity == nil {
		return ActivityCreateWithReminderOutput{}, MapToolError(errors.New("activity service returned no activity"))
	}
	assigneeMemberID := ""
	if assignee != nil {
		assigneeMemberID = *assignee
	}
	reminder, err := ReminderCreate(ctx, resolver, reminderService, ReminderCreateInput{
		Space: activity.SpaceID, Title: input.Reminder.Title, Description: input.Reminder.Description,
		ScheduledAt: scheduledAt.Format(time.RFC3339Nano), AssigneeMemberID: assigneeMemberID,
	})
	if err != nil {
		//nolint:nilerr // Reminder failure is reported as partial success after activity persistence.
		return ActivityCreateWithReminderOutput{
			Status: "partial_success", Activity: activity, ReminderError: err.Error(),
		}, nil
	}
	return ActivityCreateWithReminderOutput{Status: "created", Activity: activity, Reminder: &reminder}, nil
}
