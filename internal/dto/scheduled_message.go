package dto

import (
	"time"

	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
)

type ScheduledMessageCreateInput struct {
	Space             string
	Target            domainscheduledmessage.TargetKind
	RecipientMemberID *string
	SpaceChannelID    *string
	ContentMode       domainscheduledmessage.ContentMode
	Content           string
	Recurrence        domainscheduledmessage.Recurrence
	Timezone          string
	StartAt           time.Time
}

type ScheduledMessageUpdateInput struct {
	ContentMode *domainscheduledmessage.ContentMode
	Content     *string
	Recurrence  *domainscheduledmessage.Recurrence
	Timezone    *string
	StartAt     *time.Time
}

type ScheduledMessageListInput struct {
	Space          string                                 `json:"space,omitempty"`
	ScheduleID     *string                                `json:"schedule_id,omitempty"`
	Status         *domainscheduledmessage.ScheduleStatus `json:"status,omitempty"`
	Limit          int                                    `json:"-"`
	ViewerMemberID string                                 `json:"-"`
	CanViewAllDMs  bool                                   `json:"-"`
}

type ScheduledMessageOutput struct {
	ID                     string                                `json:"id"`
	SpaceID                string                                `json:"space_id"`
	CreatedByMemberID      string                                `json:"created_by_member_id"`
	Target                 domainscheduledmessage.TargetKind     `json:"target"`
	RecipientMemberID      *string                               `json:"recipient_member_id,omitempty"`
	SpaceChannelID         *string                               `json:"space_channel_id,omitempty"`
	ContentMode            domainscheduledmessage.ContentMode    `json:"content_mode"`
	Content                string                                `json:"content"`
	Recurrence             domainscheduledmessage.Recurrence     `json:"recurrence"`
	Timezone               string                                `json:"timezone"`
	StartAt                time.Time                             `json:"start_at"`
	NextScheduledAt        *time.Time                            `json:"next_scheduled_at,omitempty"`
	Status                 domainscheduledmessage.ScheduleStatus `json:"status"`
	UnsubscribedByMemberID *string                               `json:"unsubscribed_by_member_id,omitempty"`
	UnsubscribedAt         *time.Time                            `json:"unsubscribed_at,omitempty"`
	History                []ScheduledMessageDeliveryOutput      `json:"history,omitempty"`
	CreatedAt              time.Time                             `json:"created_at"`
	UpdatedAt              time.Time                             `json:"updated_at"`
}

type ScheduledMessageDeliveryOutput struct {
	ID                 string                                `json:"id"`
	ScheduledMessageID string                                `json:"scheduled_message_id"`
	ScheduledAt        time.Time                             `json:"scheduled_at"`
	ContentSnapshot    *string                               `json:"content_snapshot,omitempty"`
	GenerationAttempts int                                   `json:"generation_attempts"`
	DeliveryAttempts   int                                   `json:"delivery_attempts"`
	NextAttemptAt      *time.Time                            `json:"next_attempt_at,omitempty"`
	Status             domainscheduledmessage.DeliveryStatus `json:"status"`
	FailureCategory    *string                               `json:"failure_category,omitempty"`
	SentAt             *time.Time                            `json:"sent_at,omitempty"`
}

type ScheduledMessageTargetOutput struct {
	Kind           domainscheduledmessage.TargetKind `json:"kind"`
	MemberID       *string                           `json:"member_id,omitempty"`
	SpaceChannelID *string                           `json:"space_channel_id,omitempty"`
	DisplayName    string                            `json:"display_name"`
}

type ScheduledMessageGenerationInput struct {
	Theme          string
	RecipientName  string
	ScheduledAt    time.Time
	Timezone       string
	RecentMessages []string
}

type ScheduledMessageSchedulerOptions struct {
	Now func() time.Time
}

type SpaceChannelBindInput struct {
	Space           string
	CurrentChannel  string `json:"-"`
	CurrentChatID   string `json:"-"`
	CurrentChatType string `json:"-"`
}

type SpaceChannelOutput struct {
	ID              string                                    `json:"id"`
	SpaceID         string                                    `json:"space_id"`
	Channel         string                                    `json:"channel"`
	Status          domainscheduledmessage.SpaceChannelStatus `json:"status"`
	BoundByMemberID string                                    `json:"bound_by_member_id"`
	CreatedAt       time.Time                                 `json:"created_at"`
	UpdatedAt       time.Time                                 `json:"updated_at"`
}
