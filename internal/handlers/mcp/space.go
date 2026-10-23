package mcp

import (
	"context"
	"errors"

	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacespace "family-assistant/internal/interfaces/space"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SpaceGetMembersInput struct {
	Space string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
}

type SpaceCreateInput struct {
	Name     string `json:"name" jsonschema:"user-facing Space name"`
	Category string `json:"category" jsonschema:"one of family, friends, community, work, finance, custom"`
}

type SpaceUpdateInput struct {
	Space    string  `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name"`
	Name     *string `json:"name,omitempty" jsonschema:"optional replacement Space name"`
	Category *string `json:"category,omitempty" jsonschema:"optional replacement category"`
}

type SpaceArchiveInput struct {
	Space string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name"`
}

type MemberUpdateRoleInput struct {
	Space    string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name"`
	MemberID string `json:"member_id" jsonschema:"member UUID"`
	Role     string `json:"role" jsonschema:"space_admin, space_member, or space_viewer"`
}

type MemberRemoveInput struct {
	Space    string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name"`
	MemberID string `json:"member_id" jsonschema:"member UUID"`
}

// MembershipOutput omits user_email, user_id and role_id: group chats share one
// LLM session, so membership results must not leak contact details or internal IDs.
type MembershipOutput struct {
	ID            string `json:"id"`
	SpaceID       string `json:"space_id"`
	SpaceName     string `json:"space_name"`
	SpaceType     string `json:"space_type"`
	SpaceCategory string `json:"space_category"`
	UserName      string `json:"user_name"`
	RoleName      string `json:"role_name"`
	Status        string `json:"status"`
}

type SpaceListOutput struct {
	Spaces []MembershipOutput `json:"spaces"`
}

type SpaceMembersOutput struct {
	Members []MembershipOutput `json:"members"`
}

func membershipOutput(membership domainspace.ResolvedMembership) MembershipOutput {
	return MembershipOutput{
		ID: membership.ID, SpaceID: membership.SpaceID, SpaceName: membership.SpaceName,
		SpaceType: membership.SpaceType, SpaceCategory: membership.SpaceCategory,
		UserName: membership.UserName, RoleName: membership.RoleName, Status: membership.Status,
	}
}

func membershipOutputs(memberships []domainspace.ResolvedMembership) []MembershipOutput {
	output := make([]MembershipOutput, 0, len(memberships))
	for _, membership := range memberships {
		output = append(output, membershipOutput(membership))
	}
	return output
}

func membershipResult(membership *domainspace.ResolvedMembership) (MembershipOutput, error) {
	if membership == nil {
		return MembershipOutput{}, MapToolError(errors.New("space service returned no membership"))
	}
	return membershipOutput(*membership), nil
}

func SpaceList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface) ([]MembershipOutput, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("space service is not configured"))
	}
	memberships, err := service.List(ctx, actor.UserID)
	if err != nil {
		return nil, MapToolError(err)
	}
	return membershipOutputs(memberships), nil
}

func SpaceCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface, input SpaceCreateInput) (*domainspace.Space, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("space service is not configured"))
	}
	created, err := service.Create(ctx, actor.UserID, dto.SpaceCreateInput{
		Name: input.Name, Category: input.Category,
	})
	if err != nil {
		return nil, MapToolError(err)
	}
	return created, nil
}

func SpaceGetMembers(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface, input SpaceGetMembersInput) ([]MembershipOutput, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("space service is not configured"))
	}
	selected, err := selectActorSpace(ctx, resolver, actor, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	members, err := service.Members(ctx, actor.UserID, selected.SpaceID)
	if err != nil {
		return nil, MapToolError(err)
	}
	return membershipOutputs(members), nil
}

func SpaceUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface, input SpaceUpdateInput) (*domainspace.Space, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if input.Name == nil && input.Category == nil {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "update", Reason: "at least one field is required"})
	}
	if service == nil {
		return nil, MapToolError(errors.New("space service is not configured"))
	}
	updated, err := service.Update(ctx, actor.UserID, actor.SpaceID, dto.SpaceUpdateInput{Name: input.Name, Category: input.Category})
	if err != nil {
		return nil, MapToolError(err)
	}
	return updated, nil
}

func SpaceArchive(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface, input SpaceArchiveInput) (*domainspace.Space, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("space service is not configured"))
	}
	archived, err := service.Archive(ctx, actor.UserID, actor.SpaceID)
	if err != nil {
		return nil, MapToolError(err)
	}
	return archived, nil
}

func MemberUpdateRole(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface, input MemberUpdateRoleInput) (MembershipOutput, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return MembershipOutput{}, MapToolError(err)
	}
	memberID, err := utils.ParseUUID(input.MemberID, "member_id", true)
	if err != nil {
		return MembershipOutput{}, MapToolError(err)
	}
	if service == nil {
		return MembershipOutput{}, MapToolError(errors.New("space service is not configured"))
	}
	updated, err := service.UpdateMemberRole(ctx, actor.UserID, actor.SpaceID, memberID, dto.MemberRoleUpdateInput{Role: input.Role})
	if err != nil {
		return MembershipOutput{}, MapToolError(err)
	}
	return membershipResult(updated)
}

func MemberRemove(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface, input MemberRemoveInput) (MembershipOutput, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return MembershipOutput{}, MapToolError(err)
	}
	memberID, err := utils.ParseUUID(input.MemberID, "member_id", true)
	if err != nil {
		return MembershipOutput{}, MapToolError(err)
	}
	if service == nil {
		return MembershipOutput{}, MapToolError(errors.New("space service is not configured"))
	}
	removed, err := service.RemoveMember(ctx, actor.UserID, actor.SpaceID, memberID)
	if err != nil {
		return MembershipOutput{}, MapToolError(err)
	}
	return membershipResult(removed)
}

func registerSpaceTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "space_create", Description: "Create a shared Space for an allowed category: family, friends, community, work, finance, or custom.",
	}, func(ctx context.Context, input SpaceCreateInput) (*domainspace.Space, error) {
		return SpaceCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "space_list", Description: "List the authenticated user's active Spaces.",
	}, func(ctx context.Context, _ struct{}) (SpaceListOutput, error) {
		output, err := SpaceList(ctx, resolver, service)
		return SpaceListOutput{Spaces: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "space_get_members", Description: "List members of an authorized Space.",
	}, func(ctx context.Context, input SpaceGetMembersInput) (SpaceMembersOutput, error) {
		output, err := SpaceGetMembers(ctx, resolver, service, input)
		return SpaceMembersOutput{Members: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "space_update", Description: "Update the name or category of an authorized shared Space.",
	}, func(ctx context.Context, input SpaceUpdateInput) (*domainspace.Space, error) {
		return SpaceUpdate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "space_archive", Description: "Archive an authorized shared Space.",
	}, func(ctx context.Context, input SpaceArchiveInput) (*domainspace.Space, error) {
		return SpaceArchive(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "member_update_role", Description: "Change an authorized shared Space member role to admin, member, or viewer.",
	}, func(ctx context.Context, input MemberUpdateRoleInput) (MembershipOutput, error) {
		return MemberUpdateRole(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "member_remove", Description: "Deactivate a member from an authorized shared Space.",
	}, func(ctx context.Context, input MemberRemoveInput) (MembershipOutput, error) {
		return MemberRemove(ctx, resolver, service, input)
	})
}
