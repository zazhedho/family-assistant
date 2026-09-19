package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
)

func TestFamilyGetMemberReturnsOnlySafeMembershipSummary(t *testing.T) {
	ctx := WithActorContext(context.Background(), serviceidentity.ActorContext{
		UserID:          "user-1",
		MemberID:        "member-1",
		FamilyID:        "family-1",
		Source:          "mcp",
		RoleID:          "role-parent",
		RoleName:        "parent",
		HermesProfileID: "profile-parent",
		Permissions:     map[string]struct{}{"reminders:delete": {}},
	})

	got, err := FamilyGetMember(ctx)
	if err != nil {
		t.Fatalf("family_get_member: %v", err)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if string(encoded) != `{"member_id":"member-1","family_id":"family-1","role":"parent"}` {
		t.Fatalf("unexpected summary: %s", encoded)
	}
}

func TestFamilyGetMemberRejectsMissingActor(t *testing.T) {
	_, err := FamilyGetMember(context.Background())
	if !errors.Is(err, serviceidentity.ErrUnauthenticated) {
		t.Fatalf("expected unauthenticated MCP error, got %v", err)
	}
}
