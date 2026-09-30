package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"family-assistant/pkg/logger"
	"family-assistant/utils"
)

const maxMCPLogAttributes = 32

func logMCPToolCall(ctx context.Context, toolName string, input, output any, duration time.Duration, callErr error) {
	level := logger.LogLevelInfo
	if callErr != nil {
		level = logger.LogLevelWarn
	}
	logger.WriteLogWithAttrs(ctx, level, "MCP tool call completed", mcpToolLogAttrs(toolName, input, output, duration, callErr)...)
}

func mcpToolLogAttrs(toolName string, input, output any, duration time.Duration, callErr error) []slog.Attr {
	callStatus := "success"
	if callErr != nil {
		callStatus = "error"
	}
	attrs := []slog.Attr{
		slog.String("tool_name", toolName),
		slog.String("call_status", callStatus),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}
	if callErr != nil {
		attrs = append(attrs, slog.String("error_type", fmt.Sprintf("%T", callErr)))
	}
	attrs = appendSafeMCPLogAttributes(attrs, "input", input)
	return appendSafeMCPLogAttributes(attrs, "output", output)
}

func appendSafeMCPLogAttributes(attrs []slog.Attr, prefix string, value any) []slog.Attr {
	if value == nil || len(attrs) >= maxMCPLogAttributes {
		return attrs
	}
	payload := utils.JsonEncode(value)
	if payload == "" {
		return attrs
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return attrs
	}
	return collectSafeMCPLogAttributes(attrs, prefix, "", decoded)
}

func collectSafeMCPLogAttributes(attrs []slog.Attr, prefix, path string, value any) []slog.Attr {
	if len(attrs) >= maxMCPLogAttributes {
		return attrs
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return attrs
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fieldValue := fields[key]
		fieldPath := key
		if path != "" {
			fieldPath = path + "_" + key
		}
		if safeMCPLogField(key) {
			if safeValue, ok := safeMCPLogValue(fieldValue); ok {
				attrs = append(attrs, slog.Any(prefix+"_"+fieldPath, safeValue))
				continue
			}
		}
		attrs = collectSafeMCPLogAttributes(attrs, prefix, fieldPath, fieldValue)
		if len(attrs) >= maxMCPLogAttributes {
			break
		}
	}
	return attrs
}

func safeMCPLogField(key string) bool {
	switch key {
	case "id", "space_id", "user_id", "member_id", "assignee_member_id", "activity_id", "reminder_id", "invitation_id",
		"status", "kind", "category", "role", "role_name", "occurred_at", "scheduled_at", "after_minutes", "clear_assignee",
		"from", "to", "limit":
		return true
	default:
		return false
	}
}

func safeMCPLogValue(value any) (any, bool) {
	switch value := value.(type) {
	case string:
		value = strings.Map(func(character rune) rune {
			if character < 0x20 || character == 0x7f {
				return ' '
			}
			return character
		}, strings.TrimSpace(value))
		runes := []rune(value)
		if len(runes) > 128 {
			value = string(runes[:128])
		}
		return value, value != ""
	case bool, json.Number:
		return value, true
	default:
		return nil, false
	}
}
