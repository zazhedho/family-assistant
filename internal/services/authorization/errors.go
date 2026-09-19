package authorization

import (
	"errors"
	"fmt"
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
		return fmt.Sprintf("%s: %s", ErrInvalidResource, e.Reason)
	}
	return fmt.Sprintf("%s: %s %s", ErrInvalidResource, e.Field, e.Reason)
}

func (*ValidationError) Unwrap() error {
	return ErrInvalidResource
}
