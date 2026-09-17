package authorization

import (
	"errors"
	"testing"
)

func TestValidateResource(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		wantErr  bool
	}{
		{
			name: "personal owner and family are present",
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "member-1",
				Scope:         ScopePersonal,
			},
		},
		{
			name: "personal owner is missing",
			resource: Resource{
				FamilyID: "family-1",
				Scope:    ScopePersonal,
			},
			wantErr: true,
		},
		{
			name: "personal owner is whitespace",
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: " \t",
				Scope:         ScopePersonal,
			},
			wantErr: true,
		},
		{
			name: "personal family is missing",
			resource: Resource{
				OwnerMemberID: "member-1",
				Scope:         ScopePersonal,
			},
			wantErr: true,
		},
		{
			name: "personal family is whitespace",
			resource: Resource{
				FamilyID:      " \t",
				OwnerMemberID: "member-1",
				Scope:         ScopePersonal,
			},
			wantErr: true,
		},
		{
			name: "family scope has family",
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "member-1",
				Scope:         ScopeFamily,
			},
		},
		{
			name: "family scope has no family",
			resource: Resource{
				OwnerMemberID: "member-1",
				Scope:         ScopeFamily,
			},
			wantErr: true,
		},
		{
			name: "family scope family is whitespace",
			resource: Resource{
				FamilyID:      " \t",
				OwnerMemberID: "member-1",
				Scope:         ScopeFamily,
			},
			wantErr: true,
		},
		{
			name: "unknown scope",
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "member-1",
				Scope:         Scope("UNKNOWN"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResource(tt.resource)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidateResource() error = %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("ValidateResource() error = nil, want typed validation error")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("ValidateResource() error = %T %v, want *ValidationError", err, err)
			}
		})
	}
}
