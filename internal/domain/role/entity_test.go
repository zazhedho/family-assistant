package domainrole

import "testing"

func TestTableNames(t *testing.T) {
	tests := map[string]string{
		"roles":            (Role{}).TableName(),
		"role_permissions": (RolePermission{}).TableName(),
		"role_menus":       (RoleMenu{}).TableName(),
	}

	for want, got := range tests {
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	}
}
