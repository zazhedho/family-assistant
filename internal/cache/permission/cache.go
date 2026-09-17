package permissioncache

import (
	"context"
	"encoding/json"
	"starter-kit/utils"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "permission:user:"
	scanCount = int64(100)
)

func Key(userID string) string {
	return keyPrefix + userID
}

func GetUserPermissionKeys(ctx context.Context, client *redis.Client, userID string) ([]string, bool) {
	if client == nil {
		return nil, false
	}

	raw, err := client.Get(ctx, Key(userID)).Result()
	if err != nil {
		return nil, false
	}

	var permissionKeys []string
	if err := json.Unmarshal([]byte(raw), &permissionKeys); err != nil {
		return nil, false
	}
	return permissionKeys, true
}

func SetUserPermissionKeys(ctx context.Context, client *redis.Client, userID string, permissionKeys []string) {
	if client == nil {
		return
	}

	raw, err := json.Marshal(permissionKeys)
	if err != nil {
		return
	}
	_ = client.Set(ctx, Key(userID), string(raw), TTL()).Err()
}

func DeleteUserPermissionKeys(ctx context.Context, client *redis.Client, userIDs ...string) {
	if client == nil {
		return
	}

	keys := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		keys = append(keys, Key(userID))
	}
	if len(keys) == 0 {
		return
	}

	_ = client.Del(ctx, keys...).Err()
}

func DeleteAllUserPermissionKeys(ctx context.Context, client *redis.Client) {
	if client == nil {
		return
	}

	var cursor uint64
	for {
		keys, nextCursor, err := client.Scan(ctx, cursor, keyPrefix+"*", scanCount).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = client.Del(ctx, keys...).Err()
		}
		if nextCursor == 0 {
			return
		}
		cursor = nextCursor
	}
}

func TTL() time.Duration {
	fallback := time.Duration(utils.GetEnv("PERMISSION_CACHE_TTL_SECONDS", 300)) * time.Second
	if fallback <= 0 {
		fallback = 300 * time.Second
	}
	return utils.DurationFromEnv([]string{"PERMISSION_CACHE_TTL"}, fallback)
}
