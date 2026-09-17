package repositorygeneric

import "testing"

func TestContains(t *testing.T) {
	if !contains([]string{"name", "created_at"}, "name") {
		t.Fatal("expected value to be found")
	}
	if contains([]string{"name"}, "email") {
		t.Fatal("expected value to be missing")
	}
}

func TestIsSliceValue(t *testing.T) {
	if !isSliceValue([]string{"a", "b"}) {
		t.Fatal("expected string slice to be detected")
	}
	if isSliceValue([]byte("abc")) {
		t.Fatal("expected byte slice to be ignored")
	}
	if isSliceValue("abc") {
		t.Fatal("expected scalar to be ignored")
	}
}

func TestZeroValue(t *testing.T) {
	if got := zeroValue[string](); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
	type sample struct{ Name string }
	if got := zeroValue[sample](); got.Name != "" {
		t.Fatalf("expected zero struct, got %+v", got)
	}
}

func TestColumnIdentifierSafety(t *testing.T) {
	validColumns := []string{"name", "users.email", "_internal_id", "role_id2"}
	for _, column := range validColumns {
		if !isSafeColumnIdentifier(column) {
			t.Fatalf("expected %q to be safe", column)
		}
	}

	invalidColumns := []string{"", "1name", "name;", "users..email", "name OR 1=1", "name DESC"}
	for _, column := range invalidColumns {
		if isSafeColumnIdentifier(column) {
			t.Fatalf("expected %q to be unsafe", column)
		}
	}

	if got := safeColumnIdentifiers([]string{"name", "bad column", "users.email"}); len(got) != 2 {
		t.Fatalf("expected 2 safe columns, got %v", got)
	}
}
