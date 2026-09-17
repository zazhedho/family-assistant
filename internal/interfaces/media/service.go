package interfacemedia

import (
	"context"
	"mime/multipart"

	"family-assistant/internal/authscope"
	domainmedia "family-assistant/internal/domain/media"
)

type ServiceMediaInterface interface {
	MaxFileSize() int64
	Upload(ctx context.Context, ownerUserID string, file multipart.File, header *multipart.FileHeader) (domainmedia.Media, error)
	Delete(ctx context.Context, scope authscope.Scope, id string) (domainmedia.Media, error)
}
