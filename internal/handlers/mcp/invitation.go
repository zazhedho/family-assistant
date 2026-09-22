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

	"github.com/google/uuid"
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

type InvitationListInput struct {
	Space string `json:"space,omitempty" jsonschema:"shared Space UUID or exact name"`
}

type InvitationListOutput struct {
	Invitations []domaininvitation.Invitation `json:"invitations"`
}

type InvitationRevokeInput struct {
	Space        string `json:"space,omitempty" jsonschema:"shared Space UUID or exact name"`
	InvitationID string `json:"invitation_id" jsonschema:"invitation UUID"`
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

func InvitationList(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceinvitation.ServiceInvitationInterface, input InvitationListInput) ([]domaininvitation.Invitation, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("invitation service is not configured"))
	}
	selected, err := serviceidentity.SelectSpace(ctx, actor, input.Space, selectorPermissions(resolver))
	if err != nil {
		return nil, MapToolError(err)
	}
	invitations, err := service.List(ctx, actor.UserID, selected.SpaceID)
	if err != nil {
		return nil, MapToolError(err)
	}
	if invitations == nil {
		invitations = []domaininvitation.Invitation{}
	}
	return invitations, nil
}

func InvitationRevoke(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceinvitation.ServiceInvitationInterface, input InvitationRevokeInput) (*domaininvitation.Invitation, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("invitation service is not configured"))
	}
	selected, err := serviceidentity.SelectSpace(ctx, actor, input.Space, selectorPermissions(resolver))
	if err != nil {
		return nil, MapToolError(err)
	}
	invitationID := strings.TrimSpace(input.InvitationID)
	if _, err := uuid.Parse(invitationID); err != nil {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "invitation_id", Reason: "must be a valid UUID"})
	}
	revoked, err := service.Revoke(ctx, actor.UserID, selected.SpaceID, invitationID)
	if err != nil {
		return nil, MapToolError(err)
	}
	return revoked, nil
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
	addTool(server, &mcpsdk.Tool{
		Name: "invitation_list", Description: "List pending invitations for an authorized shared Space without exposing invitation tokens.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input InvitationListInput) (*mcpsdk.CallToolResult, InvitationListOutput, error) {
		output, err := InvitationList(ctx, resolver, service, input)
		return nil, InvitationListOutput{Invitations: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "invitation_revoke", Description: "Revoke a pending invitation in an authorized shared Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input InvitationRevokeInput) (*mcpsdk.CallToolResult, *domaininvitation.Invitation, error) {
		output, err := InvitationRevoke(ctx, resolver, service, input)
		return nil, output, err
	})
}
