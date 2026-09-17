package middlewares

import (
	"fmt"
	"net/http"
	"starter-kit/internal/authscope"
	"starter-kit/pkg/logger"
	"time"

	"github.com/gin-gonic/gin"
)

func RequestLogger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()

		ctx.Next()

		status := ctx.Writer.Status()
		latency := time.Since(start)
		path := ctx.FullPath()
		if path == "" {
			path = ctx.Request.URL.Path
		}

		userID := authscope.FromContext(ctx.Request.Context()).UserID

		msg := fmt.Sprintf(
			"[Request]; %s %s; status=%d; latency_ms=%d; ip=%s; user_id=%s",
			ctx.Request.Method,
			path,
			status,
			latency.Milliseconds(),
			ctx.ClientIP(),
			userID,
		)

		switch {
		case status >= http.StatusInternalServerError:
			logger.WriteLogWithContext(ctx, logger.LogLevelError, msg)
		case status >= http.StatusBadRequest:
			logger.WriteLogWithContext(ctx, logger.LogLevelWarn, msg)
		default:
			logger.WriteLogWithContext(ctx, logger.LogLevelInfo, msg)
		}
	}
}
