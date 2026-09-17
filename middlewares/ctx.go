package middlewares

import (
	"starter-kit/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func SetContextId() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var ctxId uuid.UUID
		if requestID := ctx.GetHeader("X-Request-ID"); requestID != "" {
			if parsedID, err := uuid.Parse(requestID); err == nil {
				ctxId = parsedID
			}
		}

		if ctxId == uuid.Nil {
			newID, err := uuid.NewV7()
			if err != nil {
				newID = uuid.New()
			}
			ctxId = newID
		}

		ctx.Set(utils.CtxKeyId, ctxId)
		ctx.Writer.Header().Set("X-Request-ID", ctxId.String())
		ctx.Next()
	}
}
