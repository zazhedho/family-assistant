package database

import (
	"context"
	"fmt"
	"starter-kit/pkg/logger"
	"starter-kit/utils"
	"time"

	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client

func InitRedis() (*redis.Client, error) {
	opt, _ := redis.ParseURL(utils.GetEnv("REDIS_URL", ""))
	if opt == nil {
		opt = &redis.Options{
			Addr:         fmt.Sprintf("%s:%s", utils.GetEnv("REDIS_HOST", "localhost"), utils.GetEnv("REDIS_PORT", "6379")),
			Password:     utils.GetEnv("REDIS_PASSWORD", ""),
			DB:           utils.GetEnv("REDIS_DB", 0),
			DialTimeout:  10 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			PoolSize:     10,
			PoolTimeout:  30 * time.Second,
		}
	}
	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		logger.WriteLog(logger.LogLevelError, fmt.Sprintf("Failed to connect to Redis: %v", err))
		return nil, err
	}

	logger.WriteLog(logger.LogLevelInfo, fmt.Sprintf("Connected to Redis at %s:%s", utils.GetEnv("REDIS_HOST", "localhost"), utils.GetEnv("REDIS_PORT", "6379")))
	RedisClient = client
	return client, nil
}

func CloseRedis() error {
	if RedisClient != nil {
		return RedisClient.Close()
	}
	return nil
}

func GetRedisClient() *redis.Client {
	return RedisClient
}
