package middlewares

import (
	"fmt"
	"github.com/zazhedho/family-assistant/pkg/logger"
	"github.com/zazhedho/family-assistant/pkg/response"
	"github.com/zazhedho/family-assistant/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ErrorHandler(c *gin.Context, err any) {
	logId := utils.GenerateLogId(c)
	logger.WriteLogWithContext(c, logger.LogLevelPanic, fmt.Sprintf("RECOVERY; Error: %+v;", err))

	res := response.InternalServerError(logId)
	c.AbortWithStatusJSON(http.StatusInternalServerError, res)
}
