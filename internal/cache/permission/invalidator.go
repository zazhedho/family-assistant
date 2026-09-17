package permissioncache

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type Invalidator interface {
	DeleteUser(ctx context.Context, userIDs ...string)
	DeleteAll(ctx context.Context)
}

type redisInvalidator struct {
	client *redis.Client
}

func NewInvalidator(client *redis.Client) Invalidator {
	if client == nil {
		return nil
	}
	return redisInvalidator{client: client}
}

func (i redisInvalidator) DeleteUser(ctx context.Context, userIDs ...string) {
	DeleteUserPermissionKeys(ctx, i.client, userIDs...)
}

func (i redisInvalidator) DeleteAll(ctx context.Context) {
	DeleteAllUserPermissionKeys(ctx, i.client)
}
