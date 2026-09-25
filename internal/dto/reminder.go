package dto

import (
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
)

type ReminderCreateInput struct {
	Space            string
	Title            string
	Description      string
	ScheduledAt      time.Time
	AssigneeMemberID *string
	DeliveryProvider string
	DeliveryTarget   string
}

type ReminderListInput struct {
	Space  string
	Status *domainreminder.Status
	From   *time.Time
	To     *time.Time
}

type ReminderUpdateInput struct {
	Title            *string
	Description      *string
	ScheduledAt      *time.Time
	AssigneeMemberID *string
	ClearAssignee    bool
}
