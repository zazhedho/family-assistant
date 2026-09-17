package dto

type UpdateAppConfig struct {
	Value    string `json:"value" binding:"required"`
	IsActive *bool  `json:"is_active" binding:"omitempty"`
}
