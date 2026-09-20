package authorization

import (
	"errors"

	serviceidentity "family-assistant/internal/services/identity"
)

var (
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = serviceidentity.ErrNotFound
	ErrInvalidResource = serviceidentity.ErrInvalidResource
)

type ValidationError = serviceidentity.ValidationError
