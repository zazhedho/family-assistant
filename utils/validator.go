package utils

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"starter-kit/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type ValidateMessage struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func mapValidateMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "This field is required"
	case "email":
		return "Invalid email"
	case "alphanum":
		return "Should be alphanumeric"
	case "min":
		return "Minimum " + fe.Param()
	case "max":
		return "Maximum " + fe.Param()
	case "lte":
		return "Should be less than " + fe.Param()
	case "gte":
		return "Should be greater than " + fe.Param()
	case "ltefield":
		return "Should be less than " + fe.Param()
	case "gtefield":
		return "Should be greater than " + fe.Param()
	}

	return "Invalid value"
}

func ValidateError(err error, reflectType reflect.Type, tagName string) []ValidateMessage {
	if ve, ok := errors.AsType[validator.ValidationErrors](err); ok {
		out := make([]ValidateMessage, len(ve))
		for i, fe := range ve {
			field := fe.Field()
			if structField, ok := reflectType.FieldByName(fe.Field()); ok {
				field = structField.Tag.Get(tagName)
			}
			out[i] = ValidateMessage{field, mapValidateMessage(fe)}
		}
		return out
	}
	return []ValidateMessage{{"", err.Error()}}
}

func ValidateUUID(ctx *gin.Context, logID uuid.UUID) (string, error) {
	id := ctx.Param("id")
	if id == "" {
		res := response.Response(http.StatusBadRequest, http.StatusText(http.StatusBadRequest), logID, nil)
		res.Error = "ID parameter is required"
		ctx.JSON(http.StatusBadRequest, res)
		return "", fmt.Errorf("missing ID")
	}

	if _, err := uuid.Parse(id); err != nil {
		res := response.Response(http.StatusBadRequest, http.StatusText(http.StatusBadRequest), logID, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "ID must be a valid UUID"}
		ctx.JSON(http.StatusBadRequest, res)
		return "", fmt.Errorf("invalid UUID")
	}

	return id, nil
}
