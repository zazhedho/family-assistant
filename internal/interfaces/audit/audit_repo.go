package interfaceaudit

import (
	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	interfacegeneric "github.com/zazhedho/family-assistant/internal/interfaces/generic"
)

type RepoAuditInterface interface {
	interfacegeneric.GenericRepository[domainaudit.AuditTrail]
}
