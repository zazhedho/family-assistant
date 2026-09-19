package repositorymedia

import (
	domainmedia "family-assistant/internal/domain/media"
	interfacemedia "family-assistant/internal/interfaces/media"
	repositorygeneric "family-assistant/internal/repositories/generic"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainmedia.Media]
}

func NewMediaRepo(db *gorm.DB) interfacemedia.RepoMediaInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainmedia.Media](db)}
}

var _ interfacemedia.RepoMediaInterface = (*repo)(nil)
