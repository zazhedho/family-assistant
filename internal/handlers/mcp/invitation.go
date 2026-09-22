package mcp

import (
	"context"
	"errors"
	"strings"

	domaininvitation "family-assistant/internal/domain/invitation"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	"family-assistant/internal/dto"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type InvitationCreateInput struct {
	Space        string `json:"space,omitempty" jsonschema:"shared Space UUID or exact name"`
	RoleName     string `json:"role_name" jsonschema:"space_admin, space_member, or space_viewer"`
	InvitedEmail string `json:"invited_email,omitempty" jsonschema:"optional email; leave blank for a WhatsApp bearer invitation"`
}

type InvitationCreateOutput struct {
	Invitation *domaininvitation.Invitation `json:"invitation"`
	Token      string                       `json:"token"`
}

type InvitationAcceptInput struct {
	Token string `json:"token" jsonschema:"one-time invitation token"`
}

func InvitationCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceinvitation.ServiceInvitationInterface, input InvitationCreateInput) (InvitationCreateOutput, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return InvitationCreateOutput{}, MapToolError(err)
	}
	if service == nil {
		return InvitationCreateOutput{}, MapToolError(errors.New("invitation service is not configured"))
	}
	selected, err := serviceidentity.SelectSpace(ctx, actor, input.Space, selectorPermissions(resolver))
	if err != nil {
		return InvitationCreateOutput{}, MapToolError(err)
	}
	invitation, token, err := service.Create(ctx, actor.UserID, dto.InvitationCreateInput{
		SpaceID: selected.SpaceID, InvitedEmail: input.InvitedEmail, RoleName: input.RoleName,
	})
	if err != nil {
		return InvitationCreateOutput{}, MapToolError(err)
	}
	return InvitationCreateOutput{Invitation: invitation, Token: token}, nil
}

func InvitationAccept(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceinvitation.ServiceInvitationInterface, input InvitationAcceptInput) (*domainspace.Member, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return nil, MapToolError(err)
	}
	token := strings.TrimSpace(input.Token)
	if token == "" {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "token", Reason: "is required"})
	}
	if service == nil {
		return nil, MapToolError(errors.New("invitation service is not configured"))
	}
	member, err := service.Accept(ctx, token, domainuser.Users{Id: actor.UserID})
	if err != nil {
		return nil, MapToolError(err)
	}
	return member, nil
}

func registerInvitationTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfaceinvitation.ServiceInvitationInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "invitation_create", Description: "Create a one-time invitation for an authorized shared Space. Leave invited_email blank for a WhatsApp bearer invitation; treat the returned token as sensitive.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input InvitationCreateInput) (*mcpsdk.CallToolResult, InvitationCreateOutput, error) {
		output, err := InvitationCreate(ctx, resolver, service, input)
		return nil, output, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "invitation_accept", Description: "Accept a one-time Space invitation token for the authenticated WhatsApp account.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input InvitationAcceptInput) (*mcpsdk.CallToolResult, *domainspace.Member, error) {
		output, err := InvitationAccept(ctx, resolver, service, input)
		return nil, output, err
	})
}
