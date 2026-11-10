package servicefinance

import (
	"context"
	"strings"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
)

// rowOps are the per-resource operations updateRow drives. load fetches the
// row into the caller's variable (and is called again to refresh it after the
// writes), current reports its name and archived state.
type rowOps struct {
	load      func(id string) error
	current   func() (name string, archived bool)
	rename    func(name string) error
	archive   func(id string, at time.Time) error
	unarchive func(id string) error
}

// updateRow is the shared update flow of accounts and categories: validate,
// authorize, load, rename and/or archive, reload, audit. A missing row maps to
// ErrNotFound. Archiving is idempotent and never restamps archived_at.
func (s *service) updateRow(ctx context.Context, actor domainidentity.ActorContext, spaceID, resource, rawID string, name *string, archived *bool, ops rowOps) (err error) {
	resourceID := strings.TrimSpace(rawID)
	defer func() { s.auditResult(ctx, actor, domainaudit.ActionUpdate, resource, spaceID, resourceID, err) }()

	id, newName, err := validateUpdate(rawID, name, archived)
	if err != nil {
		return err
	}
	if err = s.configured(); err != nil {
		return err
	}
	if err = s.guard(ctx, actor, spaceID, permManage); err != nil {
		return err
	}
	if err = ops.load(id); err != nil {
		return notFound(err)
	}
	currentName, isArchived := ops.current()
	change := wantArchive(archived, isArchived)
	if name != nil && newName != currentName {
		if err = ops.rename(newName); err != nil {
			return notFound(mapUniqueViolation(err))
		}
	}
	switch change {
	case archiveSet:
		err = ops.archive(id, time.Now().UTC())
	case archiveClear:
		err = ops.unarchive(id)
	}
	if err != nil {
		return notFound(err)
	}
	return notFound(ops.load(id))
}
