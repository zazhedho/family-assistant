package mcp

import (
	"context"
	"errors"

	domainspace "family-assistant/internal/domain/space"
	serviceidentity "family-assistant/internal/services/identity"
	servicespace "family-assistant/internal/services/space"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SpaceGetMembersInput struct {
	Space string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
}

func SpaceList(ctx context.Context, resolver serviceidentity.Resolver, service servicespace.Service) ([]domainspace.ResolvedMembership, error) {
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

func SpaceGetMembers(ctx context.Context, resolver serviceidentity.Resolver, service servicespace.Service, input SpaceGetMembersInput) ([]domainspace.ResolvedMembership, error) {
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

func selectorPermissions(resolver serviceidentity.Resolver) serviceidentity.PermissionLoader {
	if resolver, ok := resolver.(*serviceidentity.ResolverService); ok && resolver != nil {
		return resolver.PermissionService
	}
	if permissions, ok := resolver.(serviceidentity.PermissionLoader); ok {
		return permissions
	}
	return nil
}

func registerSpaceTools(server *mcpsdk.Server, resolver serviceidentity.Resolver, service servicespace.Service) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "space_list", Description: "List the authenticated user's active Spaces.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, []domainspace.ResolvedMembership, error) {
		output, err := SpaceList(ctx, resolver, service)
		return nil, output, err
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "space_get_members", Description: "List members of an authorized Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input SpaceGetMembersInput) (*mcpsdk.CallToolResult, []domainspace.ResolvedMembership, error) {
		output, err := SpaceGetMembers(ctx, resolver, service, input)
		return nil, output, err
	})
}
