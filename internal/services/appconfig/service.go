package serviceappconfig

import (
	"context"
	"errors"
	domainappconfig "starter-kit/internal/domain/appconfig"
	"starter-kit/internal/dto"
	interfaceappconfig "starter-kit/internal/interfaces/appconfig"
	"starter-kit/pkg/configvalue"
	"starter-kit/pkg/filter"
	"starter-kit/utils"
	"time"

	"gorm.io/gorm"
)

type AppConfigService struct {
	Repo interfaceappconfig.RepoAppConfigInterface
}

func NewAppConfigService(repo interfaceappconfig.RepoAppConfigInterface) *AppConfigService {
	return &AppConfigService{Repo: repo}
}

func (s *AppConfigService) GetAll(ctx context.Context, params filter.BaseParams) ([]domainappconfig.AppConfig, int64, error) {
	return s.Repo.GetAll(ctx, params)
}

func (s *AppConfigService) GetByID(ctx context.Context, id string) (domainappconfig.AppConfig, error) {
	return s.Repo.GetByID(ctx, id)
}

func (s *AppConfigService) GetByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, error) {
	return s.Repo.GetByKey(ctx, configKey)
}

func (s *AppConfigService) Update(ctx context.Context, id string, req dto.UpdateAppConfig) (domainappconfig.AppConfig, error) {
	config, err := s.Repo.GetByID(ctx, id)
	if err != nil {
		return domainappconfig.AppConfig{}, err
	}

	config.Value = utils.StripHTML(req.Value)
	if req.IsActive != nil {
		config.IsActive = *req.IsActive
	}
	config.UpdatedAt = new(time.Now())

	if err := s.Repo.Update(ctx, config); err != nil {
		return domainappconfig.AppConfig{}, err
	}

	return config, nil
}

func (s *AppConfigService) GetString(ctx context.Context, configKey string, fallback string) (string, error) {
	config, found, err := s.getActiveConfigByKey(ctx, configKey)
	if err != nil {
		return fallback, err
	}
	if !found {
		return fallback, nil
	}
	return configvalue.String(config.Value, fallback), nil
}

func (s *AppConfigService) GetBool(ctx context.Context, configKey string, fallback bool) (bool, error) {
	config, found, err := s.getActiveConfigByKey(ctx, configKey)
	if err != nil {
		return fallback, err
	}
	if !found {
		return fallback, nil
	}
	return configvalue.Bool(config.Value, fallback)
}

func (s *AppConfigService) GetInt(ctx context.Context, configKey string, fallback int) (int, error) {
	config, found, err := s.getActiveConfigByKey(ctx, configKey)
	if err != nil {
		return fallback, err
	}
	if !found {
		return fallback, nil
	}
	return configvalue.Int(config.Value, fallback)
}

func (s *AppConfigService) GetDuration(ctx context.Context, configKey string, fallback time.Duration) (time.Duration, error) {
	config, found, err := s.getActiveConfigByKey(ctx, configKey)
	if err != nil {
		return fallback, err
	}
	if !found {
		return fallback, nil
	}
	return configvalue.Duration(config.Value, fallback)
}

func (s *AppConfigService) DecodeJSON(ctx context.Context, configKey string, target any) error {
	config, found, err := s.getActiveConfigByKey(ctx, configKey)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return configvalue.JSON(config.Value, target)
}

func (s *AppConfigService) IsEnabled(ctx context.Context, configKey string, fallback bool) (bool, error) {
	return s.GetBool(ctx, configKey, fallback)
}

func (s *AppConfigService) getActiveConfigByKey(ctx context.Context, configKey string) (domainappconfig.AppConfig, bool, error) {
	config, err := s.Repo.GetByKey(ctx, configKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainappconfig.AppConfig{}, false, nil
		}
		return domainappconfig.AppConfig{}, false, err
	}
	if !config.IsActive {
		return domainappconfig.AppConfig{}, false, nil
	}
	return config, true, nil
}

var _ interfaceappconfig.ServiceAppConfigInterface = (*AppConfigService)(nil)
