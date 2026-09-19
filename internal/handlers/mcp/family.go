package mcp

import (
	"context"
	"strings"

	serviceidentity "family-assistant/internal/services/identity"
)

type MemberSummary struct {
	MemberID string `json:"member_id"`
	FamilyID string `json:"family_id"`
	Role     string `json:"role"`
}

func FamilyGetMember(ctx context.Context) (MemberSummary, error) {
	actor, ok := ActorFromContext(ctx)
	if !ok || strings.TrimSpace(actor.MemberID) == "" || strings.TrimSpace(actor.FamilyID) == "" || strings.TrimSpace(actor.RoleName) == "" {
		return MemberSummary{}, serviceidentity.ErrUnauthenticated
	}

	return MemberSummary{
		MemberID: strings.TrimSpace(actor.MemberID),
		FamilyID: strings.TrimSpace(actor.FamilyID),
		Role:     strings.TrimSpace(actor.RoleName),
	}, nil
}
