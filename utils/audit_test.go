package utils

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetImpersonationMetadataReturnsNilForRegularSession(t *testing.T) {
	ctx := &gin.Context{}
	ctx.Set(CtxKeyAuthData, map[string]any{
		"user_id": "user-1",
		"role":    "staff",
	})

	got := GetImpersonationMetadata(ctx)
	if got != nil {
		t.Fatalf("expected nil metadata for regular session, got %v", got)
	}
}

func TestGetImpersonationMetadataReturnsOriginalActorData(t *testing.T) {
	ctx := &gin.Context{}
	ctx.Set(CtxKeyAuthData, map[string]any{
		"user_id":           "target-1",
		"username":          "Target User",
		"role":              "viewer",
		"is_impersonated":   true,
		"original_user_id":  "admin-1",
		"original_username": "Admin User",
		"original_role":     "admin",
	})

	got := GetImpersonationMetadata(ctx)
	if got == nil {
		t.Fatal("expected impersonation metadata")
	}

	if got["is_impersonated"] != true {
		t.Fatalf("expected impersonation flag, got %v", got["is_impersonated"])
	}
	if got["original_user_id"] != "admin-1" {
		t.Fatalf("expected original user id, got %v", got["original_user_id"])
	}
	if got["original_role"] != "admin" {
		t.Fatalf("expected original role, got %v", got["original_role"])
	}
	if got["impersonated_user_id"] != "target-1" {
		t.Fatalf("expected impersonated user id, got %v", got["impersonated_user_id"])
	}
	if got["impersonated_role"] != "viewer" {
		t.Fatalf("expected impersonated role, got %v", got["impersonated_role"])
	}
}

func TestMergeMetadataPreservesBaseAndExtra(t *testing.T) {
	base := map[string]any{"a": 1}
	extra := map[string]any{"b": 2}

	got := MergeMetadata(base, extra)
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %v", got)
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("unexpected merged metadata: %v", got)
	}
}

func TestRedactSensitivePayload(t *testing.T) {
	payload := struct {
		Email        string         `json:"email"`
		Password     string         `json:"password"`
		OTPCode      string         `json:"otp_code"`
		AccessToken  string         `json:"access_token"`
		RefreshToken string         `json:"refresh_token"`
		Metadata     map[string]any `json:"metadata"`
	}{
		Email:        "jane@example.com",
		Password:     "secret123",
		OTPCode:      "123456",
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		Metadata: map[string]any{
			"client_secret": "nested-secret",
			"provider":      "google",
		},
	}

	got, ok := RedactSensitivePayload(payload).(map[string]any)
	if !ok {
		t.Fatalf("expected normalized map, got %T", got)
	}
	for _, key := range []string{"password", "otp_code", "access_token", "refresh_token"} {
		if got[key] != "[REDACTED]" {
			t.Fatalf("expected %s to be redacted, got %v", key, got[key])
		}
	}
	if got["email"] != "jane@example.com" {
		t.Fatalf("expected safe field to remain visible, got %v", got["email"])
	}

	metadata, ok := got["metadata"].(map[string]any)
	if !ok || metadata["client_secret"] != "[REDACTED]" || metadata["provider"] != "google" {
		t.Fatalf("unexpected nested redaction result: %v", got["metadata"])
	}
}
