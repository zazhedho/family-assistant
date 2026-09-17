package mcp

import (
	"errors"
	"log/slog"

	serviceauthorization "github.com/zazhedho/family-assistant/internal/services/authorization"
	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
	"gorm.io/gorm"
)

var (
	ErrMCPUnauthenticated = errors.New("authentication required")
	ErrMCPForbidden       = errors.New("forbidden")
	ErrMCPNotFound        = errors.New("not found")
	ErrMCPInvalidInput    = errors.New("invalid input")
	ErrMCPInternal        = errors.New("internal server error")
)

type MCPError struct {
	Code    string
	Message string
	cause   error
}

func (e *MCPError) Error() string {
	if e == nil || e.Message == "" {
		return ErrMCPInternal.Error()
	}
	return e.Message
}

func (e *MCPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func MapToolError(err error, logger ...*slog.Logger) error {
	if err == nil {
		return nil
	}

	var mapped *MCPError
	if errors.As(err, &mapped) {
		return mapped
	}

	switch {
	case errors.Is(err, serviceidentity.ErrUnauthenticated):
		return &MCPError{Code: "unauthenticated", Message: ErrMCPUnauthenticated.Error(), cause: err}
	case errors.Is(err, serviceauthorization.ErrForbidden):
		return &MCPError{Code: "forbidden", Message: ErrMCPForbidden.Error(), cause: err}
	case errors.Is(err, serviceauthorization.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return &MCPError{Code: "not_found", Message: ErrMCPNotFound.Error(), cause: err}
	case errors.Is(err, serviceauthorization.ErrInvalidResource):
		return &MCPError{Code: "invalid_input", Message: ErrMCPInvalidInput.Error(), cause: err}
	default:
		log := slog.Default()
		if len(logger) > 0 && logger[0] != nil {
			log = logger[0]
		}
		log.Error("mcp tool failed", "error", err)
		return &MCPError{Code: "internal", Message: ErrMCPInternal.Error(), cause: err}
	}
}
