package interfaceappconfig

import (
	"context"
	domainappconfig "github.com/zazhedho/family-assistant/internal/domain/appconfig"
	interfacegeneric "github.com/zazhedho/family-assistant/internal/interfaces/generic"
)

type RepoAppConfigInterface interface {
	interfacegeneric.GenericRepository[domainappconfig.AppConfig]

	GetByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, error)
}
