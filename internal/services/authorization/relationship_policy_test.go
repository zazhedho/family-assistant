package authorization

import (
	"context"
	"errors"
	"testing"

	identity "family-assistant/internal/services/identity"
)

func TestRelationshipPolicy(t *testing.T) {
	const permission = "reminders:view"

	tests := []struct {
		name     string
		actor    identity.ActorContext
		resource Resource
		wantErr  error
	}{
		{
			name:  "parent own personal",
			actor: task5Actor("parent", "parent-1", "family-1", permission),
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "parent-1",
				OwnerRoleName: "parent",
				Scope:         ScopePersonal,
			},
		},
		{
			name:  "parent child personal same family",
			actor: task5Actor("parent", "parent-1", "family-1", permission),
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "child-1",
				OwnerRoleName: "child",
				Scope:         ScopePersonal,
			},
		},
		{
			name:  "parent other parent personal",
			actor: task5Actor("parent", "parent-1", "family-1", permission),
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "parent-2",
				OwnerRoleName: "parent",
				Scope:         ScopePersonal,
			},
			wantErr: ErrForbidden,
		},
		{
			name:  "child own personal",
			actor: task5Actor("child", "child-1", "family-1", permission),
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "child-1",
				OwnerRoleName: "child",
				Scope:         ScopePersonal,
			},
		},
		{
			name:  "child parent personal",
			actor: task5Actor("child", "child-1", "family-1", permission),
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "parent-1",
				OwnerRoleName: "parent",
				Scope:         ScopePersonal,
			},
			wantErr: ErrForbidden,
		},
		{
			name:  "child sibling personal",
			actor: task5Actor("child", "child-1", "family-1", permission),
			resource: Resource{
				FamilyID:      "family-1",
				OwnerMemberID: "child-2",
				OwnerRoleName: "child",
				Scope:         ScopePersonal,
			},
			wantErr: ErrForbidden,
		},
		{
			name:  "member family shared same family",
			actor: task5Actor("child", "child-1", "family-1", permission),
			resource: Resource{
				FamilyID: "family-1",
				Scope:    ScopeFamily,
			},
		},
		{
			name:  "member resource other family",
			actor: task5Actor("child", "child-1", "family-1", permission),
			resource: Resource{
				FamilyID: "family-2",
				Scope:    ScopeFamily,
			},
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAuthorizer().Authorize(context.Background(), tt.actor, permission, tt.resource)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Authorize() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Authorize() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func task5Actor(roleName, memberID, familyID, permissions string) identity.ActorContext {
	return identity.ActorContext{
		RoleName:    roleName,
		MemberID:    memberID,
		FamilyID:    familyID,
		Permissions: map[string]struct{}{permissions: {}},
	}
}
