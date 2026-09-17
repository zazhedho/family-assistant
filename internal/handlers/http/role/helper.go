package handlerrole

import (
	"errors"
	"net/http"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func roleMutationErrorResponse(logId uuid.UUID, err error) (int, *response.ApiResponse) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound, response.ErrorResponse(http.StatusNotFound, messages.MsgNotFound, logId, "role not found")
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgExists, logId, "role with this name already exists")
	}

	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "already exists"):
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgExists, logId, errMsg)
	case strings.HasPrefix(errMsg, "cannot update system"),
		strings.HasPrefix(errMsg, "cannot delete system"),
		strings.HasPrefix(errMsg, "access denied:"):
		return http.StatusForbidden, response.Forbidden(logId, messages.AccessDenied)
	case strings.HasPrefix(errMsg, "invalid permission ID:"),
		strings.Contains(errMsg, "menu access is derived"):
		return http.StatusBadRequest, response.ErrorResponse(http.StatusBadRequest, messages.MsgSomethingWrong, logId, errMsg)
	default:
		return http.StatusInternalServerError, response.InternalServerError(logId)
	}
}
