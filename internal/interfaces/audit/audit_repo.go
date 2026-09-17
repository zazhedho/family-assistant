package interfaceaudit

import (
	domainaudit "family-assistant/internal/domain/audit"
	interfacegeneric "family-assistant/internal/interfaces/generic"
)

type RepoAuditInterface interface {
	interfacegeneric.GenericRepository[domainaudit.AuditTrail]
}
