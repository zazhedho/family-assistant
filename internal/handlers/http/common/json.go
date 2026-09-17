package handlercommon

import (
	"fmt"
	"net/http"
	"reflect"

	"family-assistant/pkg/logger"
	"family-assistant/pkg/messages"
	"family-assistant/pkg/response"
	"family-assistant/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func BindJSON[T any](ctx *gin.Context, logID uuid.UUID, logPrefix string, req *T) bool {
	if err := ctx.BindJSON(req); err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; BindJSON ERROR: %s;", logPrefix, err.Error()))
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logID, nil)
		res.Error = utils.ValidateError(err, reflect.TypeOf(*req), "json")
		ctx.JSON(http.StatusBadRequest, res)
		return false
	}
	return true
}
