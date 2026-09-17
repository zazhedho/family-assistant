package handlerpermission

import (
	"errors"
	"net/http"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func permissionMutationErrorResponse(logId uuid.UUID, err error) (int, *response.ApiResponse) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound, response.ErrorResponse(http.StatusNotFound, messages.MsgNotFound, logId, "permission not found")
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgExists, logId, "permission with this name already exists")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "already exists") {
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgExists, logId, errMsg)
	}

	return http.StatusInternalServerError, response.InternalServerError(logId)
}
