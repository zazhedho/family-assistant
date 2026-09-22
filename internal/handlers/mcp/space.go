package mcp

import (
	"context"
	"errors"

	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacespace "family-assistant/internal/interfaces/space"
	serviceidentity "family-assistant/internal/services/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SpaceGetMembersInput struct {
	Space string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
}

type SpaceCreateInput struct {
	Name     string `json:"name" jsonschema:"user-facing Space name"`
	Category string `json:"category" jsonschema:"one of family, friends, community, work, finance, custom"`
}

type SpaceListOutput struct {
	Spaces []domainspace.ResolvedMembership `json:"spaces"`
}

type SpaceMembersOutput struct {
	Members []domainspace.ResolvedMembership `json:"members"`
}

func SpaceList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface) ([]domainspace.ResolvedMembership, error) {
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
	if memberships == nil {
		memberships = []domainspace.ResolvedMembership{}
	}
	return memberships, nil
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

func SpaceGetMembers(ctx context.Context, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface, input SpaceGetMembersInput) ([]domainspace.ResolvedMembership, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("space service is not configured"))
	}
	selected, err := serviceidentity.SelectSpace(ctx, actor, input.Space, selectorPermissions(resolver))
	if err != nil {
		return nil, MapToolError(err)
	}
	members, err := service.Members(ctx, actor.UserID, selected.SpaceID)
	if err != nil {
		return nil, MapToolError(err)
	}
	if members == nil {
		members = []domainspace.ResolvedMembership{}
	}
	return members, nil
}

func selectorPermissions(resolver interfaceidentity.Resolver) interfaceidentity.PermissionLoader {
	if resolver, ok := resolver.(*serviceidentity.ResolverService); ok && resolver != nil {
		return resolver.PermissionService
	}
	if permissions, ok := resolver.(interfaceidentity.PermissionLoader); ok {
		return permissions
	}
	return nil
}

func registerSpaceTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacespace.ServiceSpaceInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "space_create", Description: "Create a shared Space for an allowed category: family, friends, community, work, finance, or custom.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input SpaceCreateInput) (*mcpsdk.CallToolResult, *domainspace.Space, error) {
		output, err := SpaceCreate(ctx, resolver, service, input)
		return nil, output, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "space_list", Description: "List the authenticated user's active Spaces.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, SpaceListOutput, error) {
		output, err := SpaceList(ctx, resolver, service)
		return nil, SpaceListOutput{Spaces: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "space_get_members", Description: "List members of an authorized Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input SpaceGetMembersInput) (*mcpsdk.CallToolResult, SpaceMembersOutput, error) {
		output, err := SpaceGetMembers(ctx, resolver, service, input)
		return nil, SpaceMembersOutput{Members: output}, err
	})
}
