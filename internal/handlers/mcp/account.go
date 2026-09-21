package mcp

import (
	"context"
	"errors"
	"strings"

	interfaceonboarding "family-assistant/internal/interfaces/onboarding"
	serviceidentity "family-assistant/internal/services/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type AccountRegisterInput struct {
	Name      string `json:"name"`
	BirthDate string `json:"birth_date"`
	Consent   bool   `json:"consent"`
}

type AccountRegisterOutput struct {
	Status  string `json:"status"`
	UserID  string `json:"user_id"`
	SpaceID string `json:"space_id"`
}

func AccountRegister(ctx context.Context, registrar interfaceonboarding.ServiceOnboardingInterface, input AccountRegisterInput) (AccountRegisterOutput, error) {
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ExternalID) == "" {
		return AccountRegisterOutput{}, MapToolError(serviceidentity.ErrUnauthenticated)
	}
	if registrar == nil {
		return AccountRegisterOutput{}, MapToolError(errors.New("account registration service is not configured"))
	}
	result, err := registrar.Register(ctx, interfaceonboarding.Input{
		Name: input.Name, BirthDate: input.BirthDate, Consent: input.Consent,
		Provider: request.Provider, ExternalID: request.ExternalID, Channel: request.Channel,
	})
	if err != nil {
		return AccountRegisterOutput{}, MapToolError(err)
	}
	return AccountRegisterOutput{Status: result.Status, UserID: result.UserID, SpaceID: result.SpaceID}, nil
}

func registerAccountTools(server *mcpsdk.Server, registrar interfaceonboarding.ServiceOnboardingInterface) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "account_register",
		Description: "Ask for the user's name and birth date in YYYY-MM-DD format. Explain that the birth date is used for age-policy validation, show a summary, obtain explicit confirmation, then call with consent=true.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input AccountRegisterInput) (*mcpsdk.CallToolResult, AccountRegisterOutput, error) {
		output, err := AccountRegister(ctx, registrar, input)
		return nil, output, err
	})
}
