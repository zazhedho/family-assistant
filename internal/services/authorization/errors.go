package authorization

import (
	"errors"
)

var (
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrInvalidResource = errors.New("invalid resource")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ErrInvalidResource.Error()
	}
	if e.Field == "" {
		return ErrInvalidResource.Error() + ": " + e.Reason
	}
	return ErrInvalidResource.Error() + ": " + e.Field + " " + e.Reason
}

func (*ValidationError) Unwrap() error {
	return ErrInvalidResource
}
