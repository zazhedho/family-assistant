package interfaceuser

import (
	"context"
	domainuser "starter-kit/internal/domain/user"
	interfacegeneric "starter-kit/internal/interfaces/generic"
)

type RepoUserInterface interface {
	interfacegeneric.GenericRepository[domainuser.Users]

	GetByEmail(ctx context.Context, email string) (domainuser.Users, error)
	GetByPhone(ctx context.Context, phone string) (domainuser.Users, error)
}
