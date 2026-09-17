package utils

import (
	"maps"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func GetRequestID(ctx *gin.Context) string {
	if raw, ok := ctx.Get(CtxKeyId); ok && raw != nil {
		switch v := raw.(type) {
		case uuid.UUID:
			return v.String()
		case string:
			return strings.TrimSpace(v)
		}
	}

	return GenerateLogId(ctx).String()
}

func GetImpersonationMetadata(ctx *gin.Context) map[string]any {
	authData := GetAuthData(ctx)
	if authData == nil {
		return nil
	}

	isImpersonated, ok := authData["is_impersonated"].(bool)
	if !ok || !isImpersonated {
		return nil
	}

	return map[string]any{
		"is_impersonated":      true,
		"original_user_id":     strings.TrimSpace(InterfaceString(authData["original_user_id"])),
		"original_username":    strings.TrimSpace(InterfaceString(authData["original_username"])),
		"original_role":        strings.TrimSpace(InterfaceString(authData["original_role"])),
		"impersonated_user_id": strings.TrimSpace(InterfaceString(authData["user_id"])),
		"impersonated_user":    strings.TrimSpace(InterfaceString(authData["username"])),
		"impersonated_role":    strings.TrimSpace(InterfaceString(authData["role"])),
	}
}

func MergeMetadata(base map[string]any, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}

	merged := make(map[string]any, len(base)+len(extra))
	maps.Copy(merged, base)
	maps.Copy(merged, extra)

	return merged
}

func RedactSensitivePayload(input any) any {
	normalized := NormalizePayload(input)
	return RedactSensitiveValue(normalized)
}

func RedactSensitiveValue(input any) any {
	switch v := input.(type) {
	case map[string]any:
		return redactSensitiveMap(v)
	case []any:
		return redactSensitiveSlice(v)
	default:
		return v
	}
}

func IsSensitiveKey(key string) bool {
	k := NormalizeKey(key)
	return strings.Contains(k, "password") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "otp")
}

func redactSensitiveMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, val := range in {
		if IsSensitiveKey(k) {
			out[k] = "[REDACTED]"
			continue
		}

		out[k] = RedactSensitiveValue(val)
	}
	return out
}

func redactSensitiveSlice(values []any) []any {
	out := make([]any, 0, len(values))
	for _, val := range values {
		out = append(out, RedactSensitiveValue(val))
	}
	return out
}
