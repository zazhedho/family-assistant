package domainfamilymember

import "context"

type Repository interface {
	FindActiveByHermesProfile(ctx context.Context, profileID string) (*ResolvedMember, error)
	FindActiveByID(ctx context.Context, familyID, memberID string) (*FamilyMember, error)
}
