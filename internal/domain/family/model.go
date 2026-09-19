package domainfamily

import "time"

const (
	StatusActive   = "ACTIVE"
	StatusInactive = "INACTIVE"
)

func (Family) TableName() string {
	return "families"
}

type Family struct {
	ID        string     `json:"id" gorm:"column:id;primaryKey"`
	Name      string     `json:"name" gorm:"column:name"`
	Status    string     `json:"status" gorm:"column:status"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" gorm:"column:updated_at"`
}
