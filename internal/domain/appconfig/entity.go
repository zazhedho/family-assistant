package domainappconfig

import (
	"time"

	"gorm.io/gorm"
)

func (AppConfig) TableName() string {
	return "app_configs"
}

type AppConfig struct {
	Id          string         `json:"id" gorm:"column:id;primaryKey"`
	ConfigKey   string         `json:"config_key" gorm:"column:config_key"`
	DisplayName string         `json:"display_name" gorm:"column:display_name"`
	Category    string         `json:"category" gorm:"column:category"`
	Value       string         `json:"value" gorm:"column:value"`
	Description string         `json:"description,omitempty" gorm:"column:description"`
	IsActive    bool           `json:"is_active" gorm:"column:is_active"`
	CreatedAt   time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   *time.Time     `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}
