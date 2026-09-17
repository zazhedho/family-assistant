package interfaceaudit

import (
	"context"
	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	"github.com/zazhedho/family-assistant/internal/dto"
	"github.com/zazhedho/family-assistant/pkg/filter"
)

type ServiceAuditInterface interface {
	Store(ctx context.Context, req domainaudit.AuditEvent) error
	GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error)
	GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error)
}
