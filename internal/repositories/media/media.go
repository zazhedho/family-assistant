package repositorymedia

import (
	domainmedia "starter-kit/internal/domain/media"
	interfacemedia "starter-kit/internal/interfaces/media"
	repositorygeneric "starter-kit/internal/repositories/generic"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainmedia.Media]
}

func NewMediaRepo(db *gorm.DB) interfacemedia.RepoMediaInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainmedia.Media](db)}
}

var _ interfacemedia.RepoMediaInterface = (*repo)(nil)
