package interfaceaudit

import (
	"context"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
)

type ServiceAuditInterface interface {
	Store(ctx context.Context, req domainaudit.AuditEvent) error
	GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error)
	GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error)
}
