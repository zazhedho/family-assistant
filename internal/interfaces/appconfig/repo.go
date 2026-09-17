package interfaceappconfig

import (
	"context"
	domainappconfig "starter-kit/internal/domain/appconfig"
	interfacegeneric "starter-kit/internal/interfaces/generic"
)

type RepoAppConfigInterface interface {
	interfacegeneric.GenericRepository[domainappconfig.AppConfig]

	GetByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, error)
}
