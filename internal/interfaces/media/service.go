package interfacemedia

import (
	"context"
	"mime/multipart"

	"starter-kit/internal/authscope"
	domainmedia "starter-kit/internal/domain/media"
)

type ServiceMediaInterface interface {
	MaxFileSize() int64
	Upload(ctx context.Context, ownerUserID string, file multipart.File, header *multipart.FileHeader) (domainmedia.Media, error)
	Delete(ctx context.Context, scope authscope.Scope, id string) (domainmedia.Media, error)
}
