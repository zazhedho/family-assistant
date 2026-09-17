package dto

type PermissionCreate struct {
	Name        string `json:"name" binding:"required,min=3,max=100"`
	DisplayName string `json:"display_name" binding:"required,min=3,max=150"`
	Description string `json:"description" binding:"omitempty,max=500"`
	Resource    string `json:"resource" binding:"required,min=2,max=50"`
	Action      string `json:"action" binding:"required,min=2,max=50"`
}

type PermissionUpdate struct {
	DisplayName string `json:"display_name" binding:"omitempty,min=3,max=150"`
	Description string `json:"description" binding:"omitempty,max=500"`
	Resource    string `json:"resource" binding:"omitempty,min=2,max=50"`
	Action      string `json:"action" binding:"omitempty,min=2,max=50"`
}
