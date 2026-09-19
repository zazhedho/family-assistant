package interfacemedia

import (
	"context"

	domainmedia "family-assistant/internal/domain/media"
)

type RepoMediaInterface interface {
	Store(ctx context.Context, media domainmedia.Media) error
	GetByID(ctx context.Context, id string) (domainmedia.Media, error)
	Delete(ctx context.Context, id string) error
}
