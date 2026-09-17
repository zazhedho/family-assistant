package handlerpermission

import (
	"fmt"
	"net/http"
	"starter-kit/internal/authscope"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/internal/dto"
	handlercommon "starter-kit/internal/handlers/http/common"
	interfaceaudit "starter-kit/internal/interfaces/audit"
	interfacepermission "starter-kit/internal/interfaces/permission"
	"starter-kit/pkg/filter"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"

	"github.com/gin-gonic/gin"
)

type PermissionHandler struct {
	Service interfacepermission.ServicePermissionInterface
	handlercommon.AuditWriter
}

func NewPermissionHandler(s interfacepermission.ServicePermissionInterface, auditService interfaceaudit.ServiceAuditInterface) *PermissionHandler {
	return &PermissionHandler{
		Service:     s,
		AuditWriter: handlercommon.NewAuditWriter(auditService, "PermissionHandler"),
	}
}

func (h *PermissionHandler) Create(ctx *gin.Context) {
	var req dto.PermissionCreate
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[PermissionHandler][Create]"
	reqCtx := ctx.Request.Context()

	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Request: %+v;", logPrefix, utils.JsonEncode(req)))

	data, err := h.Service.Create(reqCtx, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionCreate,
			Resource:     "permission",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to create permission",
			ErrorMessage: err.Error(),
			AfterData:    req,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Create; Error: %+v", logPrefix, err))
		statusCode, res := permissionMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionCreate,
		Resource:   "permission",
		ResourceID: data.Id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Created permission",
		AfterData:  data,
	})

	res := response.Response(http.StatusCreated, "Permission created successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusCreated, res)
}

func (h *PermissionHandler) GetByID(ctx *gin.Context) {
	id := ctx.Param("id")
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[PermissionHandler][GetByID]"
	reqCtx := ctx.Request.Context()

	data, err := h.Service.GetByID(reqCtx, id)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetByID; Error: %+v", logPrefix, err))
		res := response.Response(http.StatusNotFound, "Permission not found", logId, nil)
		res.Error = response.Errors{Code: http.StatusNotFound, Message: "permission not found"}
		ctx.JSON(http.StatusNotFound, res)
		return
	}

	res := response.Response(http.StatusOK, "Get permission successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *PermissionHandler) GetAll(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[PermissionHandler][GetAll]"
	reqCtx := ctx.Request.Context()

	params, err := filter.GetBaseParams(ctx, "resource", "asc", 10)
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

func (h *PermissionHandler) Update(ctx *gin.Context) {
	id := ctx.Param("id")
	var req dto.PermissionUpdate
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[PermissionHandler][Update]"
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
			Resource:     "permission",
			ResourceID:   id,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to update permission",
			ErrorMessage: err.Error(),
			BeforeData:   before,
			AfterData:    req,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Update; Error: %+v", logPrefix, err))
		statusCode, res := permissionMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionUpdate,
		Resource:   "permission",
		ResourceID: data.Id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Updated permission",
		BeforeData: before,
		AfterData:  data,
	})

	res := response.Response(http.StatusOK, "Permission updated successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *PermissionHandler) Delete(ctx *gin.Context) {
	id := ctx.Param("id")
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[PermissionHandler][Delete]"
	reqCtx := ctx.Request.Context()
	before, _ := h.Service.GetByID(reqCtx, id)

	if err := h.Service.Delete(reqCtx, id); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "permission",
			ResourceID:   id,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to delete permission",
			ErrorMessage: err.Error(),
			BeforeData:   before,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Delete; Error: %+v", logPrefix, err))
		statusCode, res := permissionMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionDelete,
		Resource:   "permission",
		ResourceID: id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Deleted permission",
		BeforeData: before,
	})

	res := response.Response(http.StatusOK, "Permission deleted successfully", logId, nil)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: Permission deleted", logPrefix))
	ctx.JSON(http.StatusOK, res)
}

func (h *PermissionHandler) GetUserPermissions(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[PermissionHandler][GetUserPermissions]"
	reqCtx := ctx.Request.Context()

	scope := authscope.FromContext(reqCtx)
	if scope.UserID == "" {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; User ID not found in context", logPrefix))
		res := response.Response(http.StatusUnauthorized, "Unauthorized", logId, nil)
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	data, err := h.Service.GetUserPermissions(reqCtx, scope.UserID)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetUserPermissions; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get user permissions successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}
