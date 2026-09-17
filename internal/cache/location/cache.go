package locationcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"starter-kit/internal/dto"
	"starter-kit/pkg/logger"
	"starter-kit/utils"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultTTL = 180 * 24 * time.Hour
	prefix     = "location:"
	scanCount  = int64(100)
)

func TTL() time.Duration {
	ttl := utils.GetEnv("LOCATION_CACHE_TTL", defaultTTL)
	if ttl <= 0 {
		return defaultTTL
	}

	return ttl
}

func ProvinceKey() string {
	return "location:province"
}

func CityKey(provinceCode string) string {
	return fmt.Sprintf("location:city:%s", provinceCode)
}

func DistrictKey(cityCode string) string {
	return fmt.Sprintf("location:district:%s", cityCode)
}

func VillageKey(districtCode string) string {
	return fmt.Sprintf("location:village:%s", districtCode)
}

func Prefix() string {
	return prefix
}

func Get(ctx context.Context, client *redis.Client, cacheKey string) ([]dto.Location, bool) {
	if client == nil {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cached, err := client.Get(ctx, cacheKey).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("location cache get failed; key=%s; err=%v", cacheKey, err))
		}
		return nil, false
	}

	var locations []dto.Location
	if err := json.Unmarshal([]byte(cached), &locations); err != nil {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("location cache unmarshal failed; key=%s; err=%v", cacheKey, err))
		return nil, false
	}

	return locations, true
}

func Set(ctx context.Context, client *redis.Client, cacheKey string, locations []dto.Location) {
	if client == nil {
		return
	}

	payload, err := json.Marshal(locations)
	if err != nil {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("location cache marshal failed; key=%s; err=%v", cacheKey, err))
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := client.Set(ctx, cacheKey, payload, TTL()).Err(); err != nil {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("location cache set failed; key=%s; err=%v", cacheKey, err))
	}
}

func DeleteKeys(ctx context.Context, client *redis.Client, pattern string) {
	if client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var keys []string
	var cursor uint64
	for {
		foundKeys, nextCursor, err := client.Scan(ctx, cursor, pattern+"*", scanCount).Result()
		if err != nil {
			logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("location cache scan failed; pattern=%s; err=%v", pattern, err))
			return
		}
		keys = append(keys, foundKeys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if len(keys) == 0 {
		return
	}
	if err := client.Del(ctx, keys...).Err(); err != nil {
		logger.WriteLog(logger.LogLevelWarn, fmt.Sprintf("location cache delete failed; pattern=%s; err=%v", pattern, err))
	}
}
