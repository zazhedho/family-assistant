package interfaceuser

import (
	"context"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	interfacegeneric "family-assistant/internal/interfaces/generic"
)

type RepoUserInterface interface {
	interfacegeneric.GenericRepository[domainuser.Users]

	StoreWithPersonalSpace(ctx context.Context, user domainuser.Users, space domainspace.Space, member domainspace.Member) error
	GetByEmail(ctx context.Context, email string) (domainuser.Users, error)
	GetByPhone(ctx context.Context, phone string) (domainuser.Users, error)
}
