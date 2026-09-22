package mcp

import (
	"context"
	"errors"
	"strings"

	interfaceidentity "family-assistant/internal/interfaces/identity"
	serviceidentity "family-assistant/internal/services/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type IdentityLinkInput struct {
	Code string `json:"code" jsonschema:"single-use identity link code"`
}

type IdentityLinkOutput struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
}

func IdentityLink(ctx context.Context, service interfaceidentity.LinkService, input IdentityLinkInput) (IdentityLinkOutput, error) {
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ExternalID) == "" {
		return IdentityLinkOutput{}, MapToolError(serviceidentity.ErrUnauthenticated)
	}
	if strings.TrimSpace(input.Code) == "" {
		return IdentityLinkOutput{}, MapToolError(&serviceidentity.ValidationError{Field: "code", Reason: "is required"})
	}
	if service == nil {
		return IdentityLinkOutput{}, MapToolError(errors.New("identity link service is not configured"))
	}

	identity, err := service.Link(ctx, strings.TrimSpace(input.Code), strings.TrimSpace(request.Provider), strings.TrimSpace(request.ExternalID))
	if err != nil {
		return IdentityLinkOutput{}, MapToolError(err)
	}
	if identity == nil || strings.TrimSpace(identity.ID) == "" || strings.TrimSpace(identity.UserID) == "" {
		return IdentityLinkOutput{}, MapToolError(errors.New("identity link returned an invalid identity"))
	}
	return IdentityLinkOutput{
		ID: identity.ID, UserID: identity.UserID,
		Provider: identity.Provider, ExternalID: identity.ExternalID,
	}, nil
}

func registerIdentityTools(server *mcpsdk.Server, service interfaceidentity.LinkService) {
	addTool(server, &mcpsdk.Tool{
		Name: "identity_link", Description: "Link this Hermes profile with a one-time identity code.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input IdentityLinkInput) (*mcpsdk.CallToolResult, IdentityLinkOutput, error) {
		output, err := IdentityLink(ctx, service, input)
		return nil, output, err
	})
}
