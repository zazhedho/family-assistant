package utils

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidTime = errors.New("invalid time")

func ParseRFC3339(value, field string, required bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return nil, fmt.Errorf("%s is required: %w", field, ErrInvalidTime)
		}
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339: %w", field, ErrInvalidTime)
	}
	return &parsed, nil
}
