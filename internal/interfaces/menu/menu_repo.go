package interfacemenu

import (
	"context"
	domainmenu "github.com/zazhedho/family-assistant/internal/domain/menu"
	interfacegeneric "github.com/zazhedho/family-assistant/internal/interfaces/generic"
)

type RepoMenuInterface interface {
	interfacegeneric.GenericRepository[domainmenu.MenuItem]

	GetByName(ctx context.Context, name string) (domainmenu.MenuItem, error)
	GetActiveMenus(ctx context.Context) ([]domainmenu.MenuItem, error)
	GetUserMenus(ctx context.Context, userId string) ([]domainmenu.MenuItem, error)
}
