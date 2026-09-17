package interfaceaudit

import (
	domainaudit "starter-kit/internal/domain/audit"
	interfacegeneric "starter-kit/internal/interfaces/generic"
)

type RepoAuditInterface interface {
	interfacegeneric.GenericRepository[domainaudit.AuditTrail]
}
