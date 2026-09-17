package interfaceappconfig

import (
	"context"
	domainappconfig "starter-kit/internal/domain/appconfig"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"time"
)

type ServiceAppConfigInterface interface {
	GetAll(ctx context.Context, params filter.BaseParams) ([]domainappconfig.AppConfig, int64, error)
	GetByID(ctx context.Context, id string) (domainappconfig.AppConfig, error)
	GetByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, error)
	Update(ctx context.Context, id string, req dto.UpdateAppConfig) (domainappconfig.AppConfig, error)
	GetString(ctx context.Context, configKey string, fallback string) (string, error)
	GetBool(ctx context.Context, configKey string, fallback bool) (bool, error)
	GetInt(ctx context.Context, configKey string, fallback int) (int, error)
	GetDuration(ctx context.Context, configKey string, fallback time.Duration) (time.Duration, error)
	DecodeJSON(ctx context.Context, configKey string, target any) error
	IsEnabled(ctx context.Context, configKey string, fallback bool) (bool, error)
}
