package mcp

import (
	"errors"
	"log/slog"

	domainidentity "family-assistant/internal/domain/identity"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainreminder "family-assistant/internal/domain/reminder"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"
	servicereminder "family-assistant/internal/services/reminder"
	"family-assistant/utils"

	"gorm.io/gorm"
)

var (
	ErrMCPUnauthenticated = errors.New("authentication required")
	ErrMCPForbidden       = errors.New("forbidden")
	ErrMCPNotFound        = errors.New("not found")
	ErrMCPInvalidInput    = errors.New("invalid input")
	ErrMCPConflict        = errors.New("conflict")
	ErrMCPInternal        = errors.New("internal server error")

	// ErrMCPPhoneAlreadyRegistered carries no detail about the other account.
	ErrMCPPhoneAlreadyRegistered = errors.New("this phone number is already registered to another account; ask the admin for help")
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

	if mapped, ok := errors.AsType[*MCPError](err); ok {
		return mapped
	}

	switch {
	case errors.Is(err, serviceidentity.ErrUnauthenticated):
		return &MCPError{Code: "unauthenticated", Message: ErrMCPUnauthenticated.Error(), cause: err}
	case errors.Is(err, serviceauthorization.ErrForbidden):
		return &MCPError{Code: "forbidden", Message: ErrMCPForbidden.Error(), cause: err}
	case errors.Is(err, serviceauthorization.ErrNotFound), errors.Is(err, serviceidentity.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return &MCPError{Code: "not_found", Message: ErrMCPNotFound.Error(), cause: err}
	case errors.Is(err, serviceauthorization.ErrInvalidResource),
		errors.Is(err, utils.ErrInvalidUUID),
		errors.Is(err, utils.ErrInvalidTime),
		errors.Is(err, domainreminder.ErrInvalidStatus),
		errors.Is(err, serviceidentity.ErrInvalidResource),
		errors.Is(err, serviceidentity.ErrInvalidIdentityProvider),
		errors.Is(err, serviceidentity.ErrInvalidExternalIdentity),
		errors.Is(err, interfacescheduledmessage.ErrInvalidWhatsAppIdentity),
		errors.Is(err, domainidentity.ErrInvalidLinkToken),
		errors.Is(err, domaininvitation.ErrInvalidInvitation):
		return &MCPError{Code: "invalid_input", Message: ErrMCPInvalidInput.Error(), cause: err}
	case errors.Is(err, servicereminder.ErrConflict), errors.Is(err, interfacescheduledmessage.ErrConflict), errors.Is(err, domainidentity.ErrIdentityConflict), errors.Is(err, domaininvitation.ErrMembershipConflict):
		return &MCPError{Code: "conflict", Message: ErrMCPConflict.Error(), cause: err}
	case errors.Is(err, domainidentity.ErrPhoneAlreadyRegistered):
		return &MCPError{Code: "phone_already_registered", Message: ErrMCPPhoneAlreadyRegistered.Error(), cause: err}
	case errors.Is(err, domainidentity.ErrIdentityNotFound):
		return &MCPError{Code: "not_found", Message: ErrMCPNotFound.Error(), cause: err}
	default:
		log := slog.Default()
		if len(logger) > 0 && logger[0] != nil {
			log = logger[0]
		}
		log.Error("mcp tool failed", "error", err)
		return &MCPError{Code: "internal", Message: ErrMCPInternal.Error(), cause: err}
	}
}
