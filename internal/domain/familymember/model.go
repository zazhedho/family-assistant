package domainfamilymember

import "time"

const (
	StatusActive   = "ACTIVE"
	StatusInactive = "INACTIVE"
)

func (FamilyMember) TableName() string {
	return "family_members"
}

type FamilyMember struct {
	ID        string     `json:"id" gorm:"column:id;primaryKey"`
	FamilyID  string     `json:"family_id" gorm:"column:family_id"`
	UserID    string     `json:"user_id" gorm:"column:user_id"`
	RoleID    string     `json:"role_id" gorm:"column:role_id"`
	RoleName  string     `json:"role_name,omitempty" gorm:"column:role_name;->"`
	Status    string     `json:"status" gorm:"column:status"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" gorm:"column:updated_at"`
}

type ResolvedMember struct {
	UserID          string `json:"user_id" gorm:"column:user_id"`
	MemberID        string `json:"member_id" gorm:"column:member_id"`
	FamilyID        string `json:"family_id" gorm:"column:family_id"`
	RoleID          string `json:"role_id" gorm:"column:role_id"`
	RoleName        string `json:"role_name" gorm:"column:role_name"`
	HermesProfileID string `json:"hermes_profile_id" gorm:"column:hermes_profile_id"`
}
