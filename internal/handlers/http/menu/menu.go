package handlermenu

import (
	"fmt"
	"net/http"
	"starter-kit/internal/authscope"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/internal/dto"
	handlercommon "starter-kit/internal/handlers/http/common"
	interfaceaudit "starter-kit/internal/interfaces/audit"
	interfacemenu "starter-kit/internal/interfaces/menu"
	"starter-kit/pkg/filter"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"

	"github.com/gin-gonic/gin"
)

type MenuHandler struct {
	Service interfacemenu.ServiceMenuInterface
	handlercommon.AuditWriter
}

func NewMenuHandler(s interfacemenu.ServiceMenuInterface, auditService interfaceaudit.ServiceAuditInterface) *MenuHandler {
	return &MenuHandler{
		Service:     s,
		AuditWriter: handlercommon.NewAuditWriter(auditService, "MenuHandler"),
	}
}

func (h *MenuHandler) GetByID(ctx *gin.Context) {
	id := ctx.Param("id")
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[MenuHandler][GetByID]"
	reqCtx := ctx.Request.Context()

	data, err := h.Service.GetByID(reqCtx, id)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetByID; Error: %+v", logPrefix, err))
		res := response.Response(http.StatusNotFound, "Menu not found", logId, nil)
		res.Error = response.Errors{Code: http.StatusNotFound, Message: "menu not found"}
		ctx.JSON(http.StatusNotFound, res)
		return
	}

	res := response.Response(http.StatusOK, "Get menu successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *MenuHandler) GetAll(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[MenuHandler][GetAll]"
	reqCtx := ctx.Request.Context()

	params, err := filter.GetBaseParams(ctx, "order_index", "asc", 100)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GetBaseParams; Error: %+v", logPrefix, err))
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "invalid query parameters"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	data, total, err := h.Service.GetAll(reqCtx, params)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetAll; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.PaginationResponse(http.StatusOK, int(total), params.Page, params.Limit, logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *MenuHandler) GetActiveMenus(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[MenuHandler][GetActiveMenus]"
	reqCtx := ctx.Request.Context()

	data, err := h.Service.GetActiveMenus(reqCtx)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetActiveMenus; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get active menus successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *MenuHandler) GetUserMenus(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[MenuHandler][GetUserMenus]"
	reqCtx := ctx.Request.Context()

	scope := authscope.FromContext(reqCtx)
	if scope.UserID == "" {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; User ID not found in context", logPrefix))
		res := response.Response(http.StatusUnauthorized, "Unauthorized", logId, nil)
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	data, err := h.Service.GetUserMenus(reqCtx, scope.UserID)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetUserMenus; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get user menus successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *MenuHandler) Update(ctx *gin.Context) {
	id := ctx.Param("id")
	var req dto.MenuUpdate
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[MenuHandler][Update]"
	reqCtx := ctx.Request.Context()

	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Request: %+v;", logPrefix, utils.JsonEncode(req)))

	before, _ := h.Service.GetByID(reqCtx, id)
	data, err := h.Service.Update(reqCtx, id, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionUpdate,
			Resource:     "menu",
			ResourceID:   id,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to update menu",
			ErrorMessage: err.Error(),
			BeforeData:   before,
			AfterData:    req,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Update; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionUpdate,
		Resource:   "menu",
		ResourceID: data.Id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Updated menu",
		BeforeData: before,
		AfterData:  data,
	})

	res := response.Response(http.StatusOK, "Menu updated successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}
