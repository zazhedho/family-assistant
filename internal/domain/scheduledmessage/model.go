package scheduledmessage

import "time"

type Recurrence string

const (
	RecurrenceOnce   Recurrence = "ONCE"
	RecurrenceDaily  Recurrence = "DAILY"
	RecurrenceWeekly Recurrence = "WEEKLY"
)

type ContentMode string

const (
	ContentModeFixed ContentMode = "FIXED"
	ContentModeAI    ContentMode = "AI"
)

type TargetKind string

const (
	TargetDM    TargetKind = "DM"
	TargetGroup TargetKind = "GROUP"
)

type ScheduleStatus string

const (
	ScheduleStatusActive       ScheduleStatus = "ACTIVE"
	ScheduleStatusSent         ScheduleStatus = "SENT"
	ScheduleStatusFailed       ScheduleStatus = "FAILED"
	ScheduleStatusCancelled    ScheduleStatus = "CANCELLED"
	ScheduleStatusUnsubscribed ScheduleStatus = "UNSUBSCRIBED"
)

type DeliveryStatus string

const (
	DeliveryStatusPending   DeliveryStatus = "PENDING"
	DeliveryStatusSent      DeliveryStatus = "SENT"
	DeliveryStatusSkipped   DeliveryStatus = "SKIPPED"
	DeliveryStatusFailed    DeliveryStatus = "FAILED"
	DeliveryStatusCancelled DeliveryStatus = "CANCELLED"
)

type SpaceChannelStatus string

const (
	SpaceChannelStatusActive   SpaceChannelStatus = "ACTIVE"
	SpaceChannelStatusInactive SpaceChannelStatus = "INACTIVE"
)

func (Schedule) TableName() string { return "scheduled_messages" }

type Schedule struct {
	ID                     string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID                string         `json:"space_id" gorm:"column:space_id"`
	CreatedByMemberID      string         `json:"created_by_member_id" gorm:"column:created_by_member_id"`
	Target                 TargetKind     `json:"target" gorm:"column:target"`
	RecipientMemberID      *string        `json:"recipient_member_id,omitempty" gorm:"column:recipient_member_id"`
	SpaceChannelID         *string        `json:"space_channel_id,omitempty" gorm:"column:space_channel_id"`
	ContentMode            ContentMode    `json:"content_mode" gorm:"column:content_mode"`
	Content                string         `json:"-" gorm:"column:content"`
	Recurrence             Recurrence     `json:"recurrence" gorm:"column:recurrence"`
	Timezone               string         `json:"timezone" gorm:"column:timezone"`
	StartAt                time.Time      `json:"start_at" gorm:"column:start_at"`
	NextScheduledAt        *time.Time     `json:"next_scheduled_at,omitempty" gorm:"column:next_scheduled_at"`
	Status                 ScheduleStatus `json:"status" gorm:"column:status"`
	UnsubscribedByMemberID *string        `json:"unsubscribed_by_member_id,omitempty" gorm:"column:unsubscribed_by_member_id"`
	UnsubscribedAt         *time.Time     `json:"unsubscribed_at,omitempty" gorm:"column:unsubscribed_at"`
	CreatedAt              time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt              time.Time      `json:"updated_at" gorm:"column:updated_at"`
}

func (Delivery) TableName() string { return "scheduled_message_deliveries" }

type Delivery struct {
	ID                 string         `json:"id" gorm:"column:id;primaryKey"`
	ScheduledMessageID string         `json:"scheduled_message_id" gorm:"column:scheduled_message_id;uniqueIndex:idx_scheduled_message_deliveries_occurrence,priority:1"`
	ScheduledAt        time.Time      `json:"scheduled_at" gorm:"column:scheduled_at;uniqueIndex:idx_scheduled_message_deliveries_occurrence,priority:2"`
	ContentSnapshot    *string        `json:"-" gorm:"column:content_snapshot"`
	GenerationAttempts int            `json:"generation_attempts" gorm:"column:generation_attempts"`
	DeliveryAttempts   int            `json:"delivery_attempts" gorm:"column:delivery_attempts"`
	NextAttemptAt      *time.Time     `json:"next_attempt_at,omitempty" gorm:"column:next_attempt_at"`
	LeaseToken         *string        `json:"-" gorm:"column:lease_token"`
	LeaseExpiresAt     *time.Time     `json:"-" gorm:"column:lease_until"`
	Status             DeliveryStatus `json:"status" gorm:"column:status"`
	FailureCategory    *string        `json:"failure_category,omitempty" gorm:"column:failure_category"`
	SentAt             *time.Time     `json:"sent_at,omitempty" gorm:"column:sent_at"`
	CreatedAt          time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt          time.Time      `json:"updated_at" gorm:"column:updated_at"`
}

func (SpaceChannel) TableName() string { return "space_channels" }

type SpaceChannel struct {
	ID              string             `json:"id" gorm:"column:id;primaryKey"`
	SpaceID         string             `json:"space_id" gorm:"column:space_id"`
	Channel         string             `json:"channel" gorm:"column:channel"`
	ExternalChatID  string             `json:"-" gorm:"column:external_chat_id"`
	Status          SpaceChannelStatus `json:"status" gorm:"column:status"`
	BoundByMemberID string             `json:"bound_by_member_id" gorm:"column:bound_by_member_id"`
	CreatedAt       time.Time          `json:"created_at" gorm:"column:created_at"`
	UpdatedAt       time.Time          `json:"updated_at" gorm:"column:updated_at"`
}

// CanTransitionStatus keeps unsubscribe terminal and all other terminal schedule states closed.
func CanTransitionStatus(from, to ScheduleStatus) bool {
	if from == to {
		return true
	}
	if from != ScheduleStatusActive {
		return false
	}
	switch to {
	case ScheduleStatusSent, ScheduleStatusFailed, ScheduleStatusCancelled, ScheduleStatusUnsubscribed:
		return true
	default:
		return false
	}
}
