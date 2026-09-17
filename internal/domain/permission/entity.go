package domainpermission

import (
	"time"

	"gorm.io/gorm"
)

func (Permission) TableName() string {
	return "permissions"
}

type Permission struct {
	Id          string         `json:"id" gorm:"column:id;primaryKey"`
	Name        string         `json:"name" gorm:"column:name;unique"`
	DisplayName string         `json:"display_name" gorm:"column:display_name"`
	Description string         `json:"description,omitempty" gorm:"column:description"`
	Resource    string         `json:"resource" gorm:"column:resource"`
	Action      string         `json:"action" gorm:"column:action"`
	CreatedAt   time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   *time.Time     `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}
