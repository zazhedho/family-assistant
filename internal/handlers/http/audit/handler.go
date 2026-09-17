package handleraudit

import (
	"fmt"
	"net/http"
	interfaceaudit "starter-kit/internal/interfaces/audit"
	"starter-kit/pkg/filter"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"

	"github.com/gin-gonic/gin"
)

type AuditHandler struct {
	Service interfaceaudit.ServiceAuditInterface
}

func NewAuditHandler(s interfaceaudit.ServiceAuditInterface) *AuditHandler {
	return &AuditHandler{Service: s}
}

func (h *AuditHandler) GetAll(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[AuditHandler][GetAll]"
	reqCtx := ctx.Request.Context()

	params, err := filter.GetBaseParams(ctx, "occurred_at", "desc", 20)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GetBaseParams; Error: %+v", logPrefix, err))
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "invalid query parameters"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}
	params.Filters = filter.WhitelistStringFilter(params.Filters, []string{"actor_user_id", "actor_role", "action", "resource", "status", "request_id"})

	data, total, err := h.Service.GetAll(reqCtx, params)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetAll; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.PaginationResponse(http.StatusOK, int(total), params.Page, params.Limit, logId, data)
	ctx.JSON(http.StatusOK, res)
}

func (h *AuditHandler) GetByID(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[AuditHandler][GetByID]"
	reqCtx := ctx.Request.Context()

	id, err := utils.ValidateUUID(ctx, logId)
	if err != nil {
		return
	}

	data, err := h.Service.GetByID(reqCtx, id)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetByID; Error: %+v", logPrefix, err))
		res := response.Response(http.StatusNotFound, "Audit trail not found", logId, nil)
		res.Error = response.Errors{Code: http.StatusNotFound, Message: "audit trail not found"}
		ctx.JSON(http.StatusNotFound, res)
		return
	}

	res := response.Response(http.StatusOK, "Get audit trail successfully", logId, data)
	ctx.JSON(http.StatusOK, res)
}
