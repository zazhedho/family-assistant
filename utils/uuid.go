package utils

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var ErrInvalidUUID = errors.New("must be a valid UUID")

func CreateUUID() string {
	var id string
	if uuid7, err := uuid.NewV7(); err == nil {
		id = uuid7.String()
	} else {
		id = uuid.NewString()
	}

	return id
}

func GenerateLogId(ctx *gin.Context) uuid.UUID {
	if ctx != nil {
		if storedID, ok := ctx.Get(CtxKeyId); ok {
			switch v := storedID.(type) {
			case uuid.UUID:
				return v
			case string:
				if parsedID, err := uuid.Parse(v); err == nil {
					return parsedID
				}
			}
		}
	}

	logId, err := uuid.NewV7()
	if err != nil {
		logId = uuid.New()
	}

	if ctx != nil {
		ctx.Set(CtxKeyId, logId)
	}

	return logId
}

func ParseUUID(value, field string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" && !required {
		return "", nil
	}
	if _, err := uuid.Parse(value); err != nil {
		return "", fmt.Errorf("%s: %w", field, ErrInvalidUUID)
	}
	return value, nil
}

func ParseOptionalUUID(value, field string) (*string, error) {
	parsed, err := ParseUUID(value, field, false)
	if err != nil || parsed == "" {
		return nil, err
	}
	return &parsed, nil
}

func NormalizeUUIDPointer(input string) *string {
	value, err := ParseOptionalUUID(input, "")
	if err != nil {
		return nil
	}
	return value
}
