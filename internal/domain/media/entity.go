package domainmedia

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrEmptyFile              = errors.New("file is empty")
	ErrFileTooLarge           = errors.New("file exceeds maximum size")
	ErrUnsupportedContentType = errors.New("unsupported media content type")
	ErrMediaForbidden         = errors.New("media does not belong to current user")
)

func (Media) TableName() string {
	return "media"
}

type Media struct {
	ID           string         `json:"id" gorm:"column:id;primaryKey"`
	OwnerUserID  string         `json:"owner_user_id" gorm:"column:owner_user_id"`
	ObjectKey    string         `json:"object_key" gorm:"column:object_key"`
	URL          string         `json:"url" gorm:"column:url"`
	OriginalName string         `json:"original_name" gorm:"column:original_name"`
	ContentType  string         `json:"content_type" gorm:"column:content_type"`
	Size         int64          `json:"size" gorm:"column:size"`
	CreatedAt    time.Time      `json:"created_at" gorm:"column:created_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}
