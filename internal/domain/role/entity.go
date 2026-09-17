package domainrole

import (
	"time"

	"gorm.io/gorm"
)

func (Role) TableName() string {
	return "roles"
}

type Role struct {
	Id          string         `json:"id" gorm:"column:id;primaryKey"`
	Name        string         `json:"name" gorm:"column:name;unique"`
	DisplayName string         `json:"display_name" gorm:"column:display_name"`
	Description string         `json:"description,omitempty" gorm:"column:description"`
	IsSystem    bool           `json:"is_system" gorm:"column:is_system;default:false"`
	CreatedAt   time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   *time.Time     `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func (RolePermission) TableName() string {
	return "role_permissions"
}

type RolePermission struct {
	Id           string    `json:"id" gorm:"column:id;primaryKey"`
	RoleId       string    `json:"role_id" gorm:"column:role_id"`
	PermissionId string    `json:"permission_id" gorm:"column:permission_id"`
	CreatedAt    time.Time `json:"created_at" gorm:"column:created_at"`
}

func (RoleMenu) TableName() string {
	return "role_menus"
}

type RoleMenu struct {
	Id         string    `json:"id" gorm:"column:id;primaryKey"`
	RoleId     string    `json:"role_id" gorm:"column:role_id"`
	MenuItemId string    `json:"menu_item_id" gorm:"column:menu_item_id"`
	CreatedAt  time.Time `json:"created_at" gorm:"column:created_at"`
}
