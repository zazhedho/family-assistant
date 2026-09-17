package middlewares

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"
)

// IPRateLimitMiddleware applies a simple Redis-backed rate limit per client IP
func IPRateLimitMiddleware(redisClient *redis.Client, prefix string, limit int, window time.Duration) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if redisClient == nil || limit <= 0 || window <= 0 {
			ctx.Next()
			return
		}

		logId := utils.GenerateLogId(ctx)
		logPrefix := fmt.Sprintf("[RateLimiter][%s]", prefix)

		ip := ctx.ClientIP()
		key := fmt.Sprintf("rate_limit:%s:%s", prefix, ip)

		reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 2*time.Second)
		defer cancel()

		current, err := redisClient.Incr(reqCtx, key).Result()
		if err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; redis.Incr error: %v", logPrefix, err))
			ctx.Next()
			return
		}

		if current == 1 {
			if err := redisClient.Expire(reqCtx, key, window).Err(); err != nil {
				logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; redis.Expire error: %v", logPrefix, err))
			}
		}

		if current > int64(limit) {
			ttl, _ := redisClient.TTL(reqCtx, key).Result()
			if ttl > 0 {
				ctx.Header("Retry-After", strconv.Itoa(int(ttl.Seconds())))
			}

			res := response.Response(http.StatusTooManyRequests, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{
				Code:    http.StatusTooManyRequests,
				Message: "Too many requests from this IP, please try again later",
			}
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, res)
			return
		}

		ctx.Next()
	}
}

// EndpointRateLimitMiddleware applies rate limiting per endpoint and IP
func EndpointRateLimitMiddleware(redisClient *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if redisClient == nil || limit <= 0 || window <= 0 {
			ctx.Next()
			return
		}

		logId := utils.GenerateLogId(ctx)
		logPrefix := "[EndpointRateLimiter]"

		ip := ctx.ClientIP()
		endpoint := ctx.FullPath()
		key := fmt.Sprintf("rate_limit:endpoint:%s:%s", endpoint, ip)

		reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 2*time.Second)
		defer cancel()

		current, err := redisClient.Incr(reqCtx, key).Result()
		if err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; redis.Incr error: %v", logPrefix, err))
			ctx.Next()
			return
		}

		if current == 1 {
			if err := redisClient.Expire(reqCtx, key, window).Err(); err != nil {
				logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; redis.Expire error: %v", logPrefix, err))
			}
		}

		if current > int64(limit) {
			ttl, _ := redisClient.TTL(reqCtx, key).Result()
			if ttl > 0 {
				ctx.Header("Retry-After", strconv.Itoa(int(ttl.Seconds())))
			}

			res := response.Response(http.StatusTooManyRequests, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{
				Code:    http.StatusTooManyRequests,
				Message: "Rate limit exceeded for this endpoint",
			}
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, res)
			return
		}

		ctx.Next()
	}
}
