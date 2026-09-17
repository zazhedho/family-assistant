package repositorymedia

import (
	domainmedia "github.com/zazhedho/family-assistant/internal/domain/media"
	interfacemedia "github.com/zazhedho/family-assistant/internal/interfaces/media"
	repositorygeneric "github.com/zazhedho/family-assistant/internal/repositories/generic"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainmedia.Media]
}

func NewMediaRepo(db *gorm.DB) interfacemedia.RepoMediaInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainmedia.Media](db)}
}

var _ interfacemedia.RepoMediaInterface = (*repo)(nil)
