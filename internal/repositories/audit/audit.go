package repositoryaudit

import (
	"context"
	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	interfaceaudit "github.com/zazhedho/family-assistant/internal/interfaces/audit"
	repositorygeneric "github.com/zazhedho/family-assistant/internal/repositories/generic"
	"github.com/zazhedho/family-assistant/pkg/filter"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainaudit.AuditTrail]
}

func NewAuditRepo(db *gorm.DB) interfaceaudit.RepoAuditInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainaudit.AuditTrail](db)}
}

func (r *repo) GetAll(ctx context.Context, params filter.BaseParams) (ret []domainaudit.AuditTrail, totalData int64, err error) {
	return r.GenericRepository.GetAll(ctx, params, repositorygeneric.QueryOptions{
		Search:         repositorygeneric.BuildSearchFunc("action", "resource", "status", "actor_role", "message", "error_message", "request_id", "ip_address"),
		AllowedFilters: []string{"actor_user_id", "actor_role", "action", "resource", "status", "request_id"},
		AllowedOrderColumns: []string{
			"occurred_at",
			"created_at",
			"action",
			"resource",
			"status",
			"actor_role",
		},
		DefaultOrders: []string{"occurred_at DESC", "created_at DESC"},
	})
}
