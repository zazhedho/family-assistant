package configvalue

import (
	"encoding/json"
	"fmt"
	"starter-kit/utils"
	"strconv"
	"strings"
	"time"
)

func String(raw string, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	return value
}

func Bool(raw string, fallback bool) (bool, error) {
	value := utils.NormalizeKey(raw)
	if value == "" {
		return fallback, nil
	}

	switch value {
	case "1", "true", "yes", "y", "on", "enabled":
		return true, nil
	case "0", "false", "no", "n", "off", "disabled":
		return false, nil
	default:
		return fallback, fmt.Errorf("invalid boolean value: %s", raw)
	}
}

func Int(raw string, fallback int) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback, fmt.Errorf("invalid integer value: %s", raw)
	}
	return parsed, nil
}

func Duration(raw string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback, fmt.Errorf("invalid duration value: %s", raw)
	}
	return parsed, nil
}

func JSON(raw string, target any) error {
	value := strings.TrimSpace(raw)
	if value == "" || target == nil {
		return nil
	}
	return json.Unmarshal([]byte(value), target)
}
