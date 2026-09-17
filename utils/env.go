package utils

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func GetEnv[T any](key string, def T) T {
	val, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	val = strings.TrimSpace(val)

	switch any(def).(type) {
	case int:
		if parsed, err := strconv.Atoi(val); err == nil {
			return any(parsed).(T)
		}

	case int32:
		if parsed, err := strconv.ParseInt(val, 10, 32); err == nil {
			return any(int32(parsed)).(T)
		}

	case int64:
		if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
			return any(parsed).(T)
		}

	case float32:
		if parsed, err := strconv.ParseFloat(val, 32); err == nil {
			return any(float32(parsed)).(T)
		}

	case float64:
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			return any(parsed).(T)
		}

	case bool:
		if parsed, err := strconv.ParseBool(val); err == nil {
			return any(parsed).(T)
		}

	case string:
		return any(val).(T)

	case time.Duration:
		// support:
		// - "10s", "5m", "1h" (time.ParseDuration)
		// - "60" -> 60s
		// - "eod" -> sisa durasi sampai 23:59:59 (timezone WIB +07:00)
		if strings.EqualFold(val, "eod") {
			loc := time.FixedZone("WIB", 7*3600)
			now := time.Now().In(loc)
			end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, loc)
			if end.After(now) {
				return any(end.Sub(now)).(T)
			}
			return def
		}

		if d, err := time.ParseDuration(val); err == nil {
			return any(d).(T)
		}
		if sec, err := strconv.ParseInt(val, 10, 64); err == nil {
			return any(time.Duration(sec) * time.Second).(T)
		}
	}

	return def
}

func DurationFromEnv(keys []string, fallback time.Duration) time.Duration {
	for _, key := range keys {
		value := GetEnv(key, time.Duration(0))
		if value > 0 {
			return value
		}
	}
	return fallback
}
