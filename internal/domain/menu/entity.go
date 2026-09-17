package domainmenu

import (
	"time"

	"gorm.io/gorm"
)

func (MenuItem) TableName() string {
	return "menu_items"
}

type MenuItem struct {
	Id          string         `json:"id" gorm:"column:id;primaryKey"`
	Name        string         `json:"name" gorm:"column:name;unique"`
	DisplayName string         `json:"display_name" gorm:"column:display_name"`
	Path        string         `json:"path" gorm:"column:path"`
	Icon        string         `json:"icon,omitempty" gorm:"column:icon"`
	ParentId    *string        `json:"parent_id,omitempty" gorm:"column:parent_id"`
	OrderIndex  int            `json:"order_index" gorm:"column:order_index;default:0"`
	IsActive    bool           `json:"is_active" gorm:"column:is_active;default:true"`
	CreatedAt   time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   *time.Time     `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}
