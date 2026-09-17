package utils

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestGetEnvParsesCommonTypes(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	t.Setenv("TEST_INT32", "32")
	t.Setenv("TEST_INT64", "64")
	t.Setenv("TEST_BOOL", "true")
	t.Setenv("TEST_DURATION", "90s")
	t.Setenv("TEST_DURATION_SECONDS", "120")
	t.Setenv("TEST_FLOAT", "3.5")
	t.Setenv("TEST_FLOAT32", "2.5")
	t.Setenv("TEST_STRING", " value ")

	if got := GetEnv("TEST_INT", 0); got != 42 {
		t.Fatalf("expected int 42, got %d", got)
	}
	if got := GetEnv("TEST_INT32", int32(0)); got != 32 {
		t.Fatalf("expected int32 32, got %d", got)
	}
	if got := GetEnv("TEST_INT64", int64(0)); got != 64 {
		t.Fatalf("expected int64 64, got %d", got)
	}
	if got := GetEnv("TEST_BOOL", false); !got {
		t.Fatalf("expected bool true")
	}
	if got := GetEnv("TEST_DURATION", time.Second); got != 90*time.Second {
		t.Fatalf("expected 90s, got %v", got)
	}
	if got := GetEnv("TEST_DURATION_SECONDS", time.Second); got != 120*time.Second {
		t.Fatalf("expected 120s, got %v", got)
	}
	if got := GetEnv("TEST_FLOAT", 0.0); got != 3.5 {
		t.Fatalf("expected 3.5, got %v", got)
	}
	if got := GetEnv("TEST_FLOAT32", float32(0)); got != 2.5 {
		t.Fatalf("expected float32 2.5, got %v", got)
	}
	if got := GetEnv("TEST_STRING", "fallback"); got != "value" {
		t.Fatalf("expected trimmed string, got %q", got)
	}
}

func TestGetEnvFallsBackForInvalidValue(t *testing.T) {
	t.Setenv("TEST_INT_INVALID", "not-int")
	if got := GetEnv("TEST_INT_INVALID", 7); got != 7 {
		t.Fatalf("expected fallback value, got %d", got)
	}
}

func TestConvertValuesToStringConvertsSelectedKeys(t *testing.T) {
	got := ConvertValuesToString(map[string]any{
		"id":     123,
		"active": true,
		"ids":    []any{"1", "2"},
	}, "id", "ids")

	want := map[string]any{
		"id":     "123",
		"active": true,
		"ids":    `["1","2"]`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

type stringerValue string

func (s stringerValue) String() string {
	return "stringer:" + string(s)
}

type boolAlias bool

func TestInterfaceStringAndBoolVariants(t *testing.T) {
	if got := InterfaceString(nil); got != "" {
		t.Fatalf("expected empty nil string, got %q", got)
	}
	if got := InterfaceString([]byte("bytes")); got != "bytes" {
		t.Fatalf("expected bytes string, got %q", got)
	}
	if got := InterfaceString(map[string]string{"a": "b"}); got != `{"a":"b"}` {
		t.Fatalf("expected JSON string, got %q", got)
	}

	if InterfaceBool(nil) {
		t.Fatal("expected nil to be false")
	}
	if !InterfaceBool(true) {
		t.Fatal("expected bool true")
	}
	if !InterfaceBool(" TRUE ") {
		t.Fatal("expected true string")
	}
	if !InterfaceBool(boolAlias(true)) {
		t.Fatal("expected marshaled true alias to be true")
	}
}

func TestConvertValuesToStringCoversAllKeysAndTypes(t *testing.T) {
	got := ConvertValuesToString(map[string]any{
		"nil":      nil,
		"string":   "already",
		"stringer": stringerValue("value"),
		"slice":    []string{"a", "b"},
		"error":    fmt.Errorf("boom"),
	})

	want := map[string]any{
		"nil":      "",
		"string":   "already",
		"stringer": "stringer:value",
		"slice":    "a,b",
		"error":    "boom",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
	if got := ConvertValuesToString(nil); got != nil {
		t.Fatalf("expected nil map, got %#v", got)
	}
}

func TestNormalizeUUIDPointer(t *testing.T) {
	if got := NormalizeUUIDPointer(""); got != nil {
		t.Fatalf("expected nil for empty input, got %v", *got)
	}
	if got := NormalizeUUIDPointer("not-a-uuid"); got != nil {
		t.Fatalf("expected nil for invalid uuid, got %v", *got)
	}

	id := "550e8400-e29b-41d4-a716-446655440000"
	got := NormalizeUUIDPointer(" " + id + " ")
	if got == nil || *got != id {
		t.Fatalf("expected normalized uuid pointer, got %v", got)
	}
}

func TestNormalizePhoneAndEmail(t *testing.T) {
	if got := NormalizePhoneTo62("+62 812-3456-789"); got != "628123456789" {
		t.Fatalf("unexpected phone normalization: %q", got)
	}
	if got := NormalizePhoneTo62("0812 3456 789"); got != "628123456789" {
		t.Fatalf("unexpected phone normalization: %q", got)
	}
	if got := SanitizeEmail(" Jane.Doe@Example.COM "); got != "jane.doe@example.com" {
		t.Fatalf("unexpected email sanitization: %q", got)
	}
}
