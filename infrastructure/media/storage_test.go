package media

import (
	"strings"
	"testing"
)

func TestInitStorageReturnsErrorForUnsupportedProvider(t *testing.T) {
	t.Setenv("STORAGE_PROVIDER", "unsupported")

	provider, err := InitStorage()
	if err == nil {
		t.Fatalf("expected error, got provider %#v", provider)
	}
	if !strings.Contains(err.Error(), "unsupported storage provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}
