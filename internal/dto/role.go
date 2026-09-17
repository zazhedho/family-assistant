package dto

type RoleCreate struct {
	Name        string `json:"name" binding:"required,min=3,max=50"`
	DisplayName string `json:"display_name" binding:"required,min=3,max=100"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

type RoleUpdate struct {
	DisplayName string `json:"display_name" binding:"omitempty,min=3,max=100"`
	Description string `json:"description" binding:"omitempty,max=500"`
}

type AssignPermissions struct {
	PermissionIds []string `json:"permission_ids" binding:"required,min=1"`
}

type AssignMenus struct {
	MenuIds []string `json:"menu_ids" binding:"required,min=1"`
}

type RoleWithDetails struct {
	Id            string   `json:"id"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name"`
	Description   string   `json:"description"`
	IsSystem      bool     `json:"is_system"`
	PermissionIds []string `json:"permission_ids"`
	MenuIds       []string `json:"menu_ids"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
}
