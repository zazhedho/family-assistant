package handlerlocation

import (
	"errors"
	"fmt"
	"net/http"
	"starter-kit/internal/dto"
	handlercommon "starter-kit/internal/handlers/http/common"
	interfacelocation "starter-kit/internal/interfaces/location"
	servicelocation "starter-kit/internal/services/location"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LocationHandler struct {
	Service interfacelocation.ServiceLocationInterface
}

func NewLocationHandler(s interfacelocation.ServiceLocationInterface) *LocationHandler {
	return &LocationHandler{Service: s}
}

func (h *LocationHandler) GetProvince(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[LocationHandler][GetProvince]"
	reqCtx := ctx.Request.Context()

	data, err := h.Service.GetProvince(reqCtx)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetProvince; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get province successfully", logId, data)
	ctx.JSON(http.StatusOK, res)
}

func (h *LocationHandler) GetCity(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[LocationHandler][GetCity]"
	reqCtx := ctx.Request.Context()

	provinceCode := ctx.Query("province_code")
	if provinceCode == "" {
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "province_code is required"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	data, err := h.Service.GetCity(reqCtx, provinceCode)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetCity; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get city successfully", logId, data)
	ctx.JSON(http.StatusOK, res)
}

func (h *LocationHandler) GetDistrict(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[LocationHandler][GetDistrict]"
	reqCtx := ctx.Request.Context()

	cityCode := ctx.Query("city_code")
	if cityCode == "" {
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "city_code is required"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	data, err := h.Service.GetDistrict(reqCtx, cityCode)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetDistrict; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get district successfully", logId, data)
	ctx.JSON(http.StatusOK, res)
}

func (h *LocationHandler) GetVillage(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[LocationHandler][GetVillage]"
	reqCtx := ctx.Request.Context()

	districtCode := ctx.Query("district_code")
	if districtCode == "" {
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "district_code is required"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	data, err := h.Service.GetVillage(reqCtx, districtCode)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetVillage; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get village successfully", logId, data)
	ctx.JSON(http.StatusOK, res)
}

func (h *LocationHandler) Sync(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[LocationHandler][Sync]"
	reqCtx := ctx.Request.Context()

	var req dto.SyncLocationRequest
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	data, err := h.Service.StartSync(reqCtx, req)
	if err != nil {
		if errors.Is(err, servicelocation.ErrLocationSyncRunning) {
			res := response.Response(http.StatusConflict, messages.MsgSomethingWrong, logId, data)
			res.Error = response.Errors{Code: http.StatusConflict, Message: err.Error()}
			ctx.JSON(http.StatusConflict, res)
			return
		}

		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.StartSync; Error: %+v", logPrefix, err))
		res := response.Response(http.StatusBadRequest, messages.MsgSomethingWrong, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: err.Error()}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	res := response.Response(http.StatusAccepted, "Location sync started", logId, data)
	ctx.JSON(http.StatusAccepted, res)
}

func (h *LocationHandler) GetSyncJob(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[LocationHandler][GetSyncJob]"
	reqCtx := ctx.Request.Context()

	jobID := ctx.Param("id")
	if jobID == "" {
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "sync job id is required"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	data, err := h.Service.GetSyncJob(reqCtx, jobID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "location sync job not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetSyncJob; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get location sync job successfully", logId, data)
	ctx.JSON(http.StatusOK, res)
}
