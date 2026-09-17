package interfaceaudit

import (
	"context"
	domainaudit "family-assistant/internal/domain/audit"
	"family-assistant/internal/dto"
	"family-assistant/pkg/filter"
)

type ServiceAuditInterface interface {
	Store(ctx context.Context, req domainaudit.AuditEvent) error
	GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error)
	GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error)
}
