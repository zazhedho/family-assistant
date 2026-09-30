package utils

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	domainuser "family-assistant/internal/domain/user"
)

func TestJSONAndStringHelpers(t *testing.T) {
	if got := JsonEncode(map[string]string{"name": "Jane"}); !strings.Contains(got, "Jane") {
		t.Fatalf("unexpected JSON encoding: %q", got)
	}
	if got := NormalizePayload(struct {
		Name string `json:"name"`
	}{Name: "Jane"}); got == nil {
		t.Fatal("expected normalized payload")
	}
	if got := TitleCase("jane doe"); got != "Jane Doe" {
		t.Fatalf("unexpected title case: %q", got)
	}
	if got := CreateUUID(); got == "" {
		t.Fatal("expected uuid")
	}
	if got := JsonEncode(make(chan int)); got != "" {
		t.Fatalf("expected empty string for unsupported JSON value, got %q", got)
	}
	ch := make(chan int)
	if got := NormalizePayload(ch); got != ch {
		t.Fatal("expected unsupported payload to be returned unchanged")
	}
}

func TestGenerateLogIdAndRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	id := uuid.New()
	ctx.Set(CtxKeyId, id)
	if got := GenerateLogId(ctx); got != id {
		t.Fatalf("expected stored uuid, got %s", got)
	}
	if got := GetRequestID(ctx); got != id.String() {
		t.Fatalf("expected request id string, got %q", got)
	}

	ctx.Set(CtxKeyId, "not-a-uuid")
	if got := GenerateLogId(ctx); got == uuid.Nil {
		t.Fatal("expected generated uuid for invalid string")
	}

	ctx.Set(CtxKeyId, " request-id ")
	if got := GetRequestID(ctx); got != "request-id" {
		t.Fatalf("expected trimmed request id, got %q", got)
	}
}

func TestParseUUIDPreservesRequiredAndOptionalInputs(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000101"
	for _, tt := range []struct {
		name     string
		input    string
		required bool
		want     string
		invalid  bool
	}{
		{name: "blank optional"},
		{name: "blank required", required: true, invalid: true},
		{name: "invalid optional", input: "invalid", invalid: true},
		{name: "invalid required", input: "invalid", required: true, invalid: true},
		{name: "trimmed UUID", input: " " + id + " ", required: true, want: id},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUUID(tt.input, "resource_id", tt.required)
			if !tt.invalid {
				if err != nil || got != tt.want {
					t.Fatalf("ParseUUID = %q, %v; want %q", got, err, tt.want)
				}
				return
			}
			if !errors.Is(err, ErrInvalidUUID) || !strings.Contains(err.Error(), "resource_id") {
				t.Fatalf("expected UUID validation error with field context, got %v", err)
			}
		})
	}
}

func TestParseRFC3339PreservesRequiredAndOptionalInputs(t *testing.T) {
	for _, tt := range []struct {
		name     string
		input    string
		required bool
		invalid  bool
	}{
		{name: "blank optional"},
		{name: "blank required", required: true, invalid: true},
		{name: "invalid timestamp", input: "tomorrow", invalid: true},
		{name: "timestamp with timezone", input: "2026-09-30T08:00:00+07:00", required: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRFC3339(tt.input, "scheduled_at", tt.required)
			if tt.invalid {
				if !errors.Is(err, ErrInvalidTime) || !strings.Contains(err.Error(), "scheduled_at") {
					t.Fatalf("expected time validation error with field context, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse time: %v", err)
			}
			if tt.input == "" {
				if got != nil {
					t.Fatalf("expected nil for optional timestamp, got %v", got)
				}
			} else if got == nil || got.Format(time.RFC3339) != tt.input {
				t.Fatalf("timestamp changed: got %v, want %s", got, tt.input)
			}
		})
	}
}

func TestValidateErrorAndValidateUUID(t *testing.T) {
	type request struct {
		Email string `json:"email" validate:"required,email"`
	}
	validate := validator.New()
	err := validate.Struct(request{})
	got := ValidateError(err, reflect.TypeFor[request](), "json")
	if len(got) != 1 || got[0].Field != "email" || got[0].Message == "" {
		t.Fatalf("unexpected validation mapping: %+v", got)
	}

	got = ValidateError(errors.New("plain error"), reflect.TypeFor[request](), "json")
	if len(got) != 1 || got[0].Message != "plain error" {
		t.Fatalf("unexpected plain error mapping: %+v", got)
	}

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}
	if _, err := ValidateUUID(ctx, uuid.New()); err == nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid uuid response, code=%d err=%v", rec.Code, err)
	}

	rec = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(rec)
	if _, err := ValidateUUID(ctx, uuid.New()); err == nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing uuid response, code=%d err=%v", rec.Code, err)
	}

	id := uuid.NewString()
	rec = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: id}}
	gotID, err := ValidateUUID(ctx, uuid.New())
	if err != nil || gotID != id {
		t.Fatalf("expected valid uuid, id=%q err=%v", gotID, err)
	}
}

func TestValidateErrorMapsKnownTags(t *testing.T) {
	type request struct {
		Required string `json:"required" validate:"required"`
		Email    string `json:"email" validate:"email"`
		AlphaNum string `json:"alphanum" validate:"alphanum"`
		Min      string `json:"min" validate:"min=3"`
		Max      string `json:"max" validate:"max=2"`
		LTE      int    `json:"lte" validate:"lte=3"`
		GTE      int    `json:"gte" validate:"gte=3"`
		LTEField int    `json:"ltefield" validate:"ltefield=GTEField"`
		GTEField int    `json:"gtefield" validate:"gtefield=LTEField"`
		UUID     string `json:"uuid" validate:"uuid"`
	}
	validate := validator.New()
	err := validate.Struct(request{
		Email:    "bad",
		AlphaNum: "with space",
		Min:      "no",
		Max:      "too-long",
		LTE:      4,
		GTE:      2,
		LTEField: 10,
		GTEField: 1,
		UUID:     "bad",
	})
	got := ValidateError(err, reflect.TypeFor[request](), "json")
	messages := map[string]string{}
	for _, item := range got {
		messages[item.Field] = item.Message
	}

	want := map[string]string{
		"required": "This field is required",
		"email":    "Invalid email",
		"alphanum": "Should be alphanumeric",
		"min":      "Minimum 3",
		"max":      "Maximum 2",
		"lte":      "Should be less than 3",
		"gte":      "Should be greater than 3",
		"ltefield": "Should be less than GTEField",
		"gtefield": "Should be greater than LTEField",
		"uuid":     "Invalid value",
	}
	for field, message := range want {
		if messages[field] != message {
			t.Fatalf("expected %s=%q, got %q in %#v", field, message, messages[field], messages)
		}
	}
}

func TestJwtClaimsReadsAuthorizationHeader(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")
	token, err := GenerateJwt(&domainuser.Users{
		Id:   "user-1",
		Name: "Jane",
		Role: RoleViewer,
	}, "log-1")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.Header.Set("Authorization", "Bearer "+token)

	tokenString, claims, err := JwtClaims(ctx)
	if err != nil || tokenString != token || claims["user_id"] == "" {
		t.Fatalf("jwt claims: token=%q claims=%+v err=%v", tokenString, claims, err)
	}
}
