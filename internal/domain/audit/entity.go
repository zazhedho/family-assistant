package domainaudit

import "time"

func (AuditTrail) TableName() string {
	return "audit_trails"
}

type AuditTrail struct {
	ID           string    `json:"id" gorm:"column:id;primaryKey"`
	OccurredAt   time.Time `json:"occurred_at" gorm:"column:occurred_at"`
	ActorUserID  *string   `json:"actor_user_id,omitempty" gorm:"column:actor_user_id"`
	ActorRole    string    `json:"actor_role,omitempty" gorm:"column:actor_role"`
	Action       string    `json:"action" gorm:"column:action"`
	Resource     string    `json:"resource" gorm:"column:resource"`
	ResourceID   string    `json:"resource_id,omitempty" gorm:"column:resource_id"`
	Status       string    `json:"status" gorm:"column:status"`
	Message      string    `json:"message,omitempty" gorm:"column:message"`
	ErrorMessage string    `json:"error_message,omitempty" gorm:"column:error_message"`
	RequestID    string    `json:"request_id,omitempty" gorm:"column:request_id"`
	IPAddress    string    `json:"ip_address,omitempty" gorm:"column:ip_address"`
	UserAgent    string    `json:"user_agent,omitempty" gorm:"column:user_agent"`
	BeforeData   string    `json:"before_data,omitempty" gorm:"column:before_data"`
	AfterData    string    `json:"after_data,omitempty" gorm:"column:after_data"`
	Metadata     string    `json:"metadata,omitempty" gorm:"column:metadata"`
	CreatedAt    time.Time `json:"created_at" gorm:"column:created_at"`
}

type AuditEvent struct {
	OccurredAt   time.Time
	ActorUserID  string
	ActorRole    string
	Action       string
	Resource     string
	ResourceID   string
	Status       string
	Message      string
	ErrorMessage string
	RequestID    string
	IPAddress    string
	UserAgent    string
	BeforeData   any
	AfterData    any
	Metadata     map[string]any
}

const (
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionAssign  = "assign"
	ActionLogin   = "login"
	ActionLogout  = "logout"
	ActionRefresh = "refresh"
)

const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)
