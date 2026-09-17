package dto

import "time"

type AuditActor struct {
	UserID *string `json:"user_id,omitempty"`
	Role   string  `json:"role,omitempty"`
}

type AuditTrailResponse struct {
	ID            string     `json:"id"`
	OccurredAt    time.Time  `json:"occurred_at"`
	Actor         AuditActor `json:"actor"`
	Action        string     `json:"action"`
	ActionLabel   string     `json:"action_label"`
	Resource      string     `json:"resource"`
	ResourceLabel string     `json:"resource_label"`
	ResourceID    string     `json:"resource_id,omitempty"`
	Status        string     `json:"status"`
	StatusLabel   string     `json:"status_label"`
	Summary       string     `json:"summary"`
	Message       string     `json:"message,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	RequestID     string     `json:"request_id,omitempty"`
	IPAddress     string     `json:"ip_address,omitempty"`
	UserAgent     string     `json:"user_agent,omitempty"`
	BeforeData    any        `json:"before_data,omitempty"`
	AfterData     any        `json:"after_data,omitempty"`
	Metadata      any        `json:"metadata,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}
