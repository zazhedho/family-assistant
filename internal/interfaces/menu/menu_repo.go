package interfacemenu

import (
	"context"
	domainmenu "family-assistant/internal/domain/menu"
	interfacegeneric "family-assistant/internal/interfaces/generic"
)

type RepoMenuInterface interface {
	interfacegeneric.GenericRepository[domainmenu.MenuItem]

	GetByName(ctx context.Context, name string) (domainmenu.MenuItem, error)
	GetActiveMenus(ctx context.Context) ([]domainmenu.MenuItem, error)
	GetUserMenus(ctx context.Context, userId string) ([]domainmenu.MenuItem, error)
}
