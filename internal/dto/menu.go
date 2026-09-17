package dto

type MenuCreate struct {
	Name        string  `json:"name" binding:"required,min=2,max=50"`
	DisplayName string  `json:"display_name" binding:"required,min=2,max=100"`
	Path        string  `json:"path" binding:"required,max=255"`
	Icon        string  `json:"icon" binding:"omitempty,max=50"`
	ParentId    *string `json:"parent_id" binding:"omitempty"`
	OrderIndex  int     `json:"order_index" binding:"omitempty"`
	IsActive    *bool   `json:"is_active" binding:"omitempty"`
}

type MenuUpdate struct {
	DisplayName string  `json:"display_name" binding:"omitempty,min=2,max=100"`
	Path        string  `json:"path" binding:"omitempty,max=255"`
	Icon        string  `json:"icon" binding:"omitempty,max=50"`
	ParentId    *string `json:"parent_id" binding:"omitempty"`
	OrderIndex  *int    `json:"order_index" binding:"omitempty"`
	IsActive    *bool   `json:"is_active" binding:"omitempty"`
}
