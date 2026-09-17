package interfaceappconfig

import (
	"context"
	domainappconfig "family-assistant/internal/domain/appconfig"
	interfacegeneric "family-assistant/internal/interfaces/generic"
)

type RepoAppConfigInterface interface {
	interfacegeneric.GenericRepository[domainappconfig.AppConfig]

	GetByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, error)
}
