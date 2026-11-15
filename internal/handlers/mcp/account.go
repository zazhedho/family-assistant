package mcp

import (
	"context"
	"errors"
	"strings"

	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
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
	if err := requireDirectMessage(ctx); err != nil {
		return AccountRegisterOutput{}, err
	}
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ExternalID) == "" {
		return AccountRegisterOutput{}, MapToolError(serviceidentity.ErrUnauthenticated)
	}
	if registrar == nil {
		return AccountRegisterOutput{}, MapToolError(errors.New("account registration service is not configured"))
	}
	result, err := registrar.Register(ctx, dto.AccountRegistrationInput{
		Name: input.Name, BirthDate: input.BirthDate, Consent: input.Consent,
		Provider: request.Provider, ExternalID: request.ExternalID, Channel: request.Channel,
	})
	if err != nil {
		return AccountRegisterOutput{}, MapToolError(err)
	}
	return AccountRegisterOutput{Status: result.Status, UserID: result.UserID, SpaceID: result.SpaceID}, nil
}

func AccountLink(ctx context.Context, registrar interfaceonboarding.ServiceOnboardingInterface) (AccountRegisterOutput, error) {
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ExternalID) == "" {
		return AccountRegisterOutput{}, MapToolError(serviceidentity.ErrUnauthenticated)
	}
	if registrar == nil {
		return AccountRegisterOutput{}, MapToolError(errors.New("account registration service is not configured"))
	}
	result, err := registrar.LinkExisting(ctx, dto.AccountLinkInput{
		Provider: request.Provider, ExternalID: request.ExternalID, Channel: request.Channel,
	})
	if err != nil {
		return AccountRegisterOutput{}, MapToolError(err)
	}
	return AccountRegisterOutput{Status: result.Status, UserID: result.UserID, SpaceID: result.SpaceID}, nil
}

// AccountRenameInput deliberately has no user, phone or target field: the account
// renamed is always the one linked to the signed identity of the caller.
type AccountRenameInput struct {
	Name string `json:"name" jsonschema:"the exact new name the user confirmed, 3 to 100 characters"`
}

type AccountRenameOutput struct {
	Status string `json:"status" jsonschema:"renamed or unchanged"`
	Name   string `json:"name" jsonschema:"the account name now on record"`
}

func AccountRename(ctx context.Context, registrar interfaceonboarding.ServiceOnboardingInterface, input AccountRenameInput) (AccountRenameOutput, error) {
	if err := requireDirectMessage(ctx); err != nil {
		return AccountRenameOutput{}, err
	}
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ExternalID) == "" {
		return AccountRenameOutput{}, MapToolError(serviceidentity.ErrUnauthenticated)
	}
	if registrar == nil {
		return AccountRenameOutput{}, MapToolError(errors.New("account registration service is not configured"))
	}
	result, err := registrar.Rename(ctx, dto.AccountRenameInput{
		Name: input.Name, Provider: request.Provider, ExternalID: request.ExternalID, Channel: request.Channel,
	})
	if err != nil {
		if errors.Is(err, domainidentity.ErrIdentityNotFound) {
			return AccountRenameOutput{}, MapToolError(serviceidentity.ErrUnauthenticated)
		}
		return AccountRenameOutput{}, MapToolError(err)
	}
	return AccountRenameOutput{Status: result.Status, Name: result.Name}, nil
}

const accountRenameDescription = "Renames ONLY the account of the person who is chatting (the sender), never anyone else. It cannot rename another member even if the owner asks; there is no way to select another person. " +
	"Ask the user for the exact new name, show it back exactly as they typed it and get an explicit yes BEFORE calling. " +
	"Direct message only: in a group, ask them to continue in a private chat. " +
	"The name follows the registration rules: 3 to 100 characters, no hidden or control characters, no < or >; emoji are fine but joined emoji sequences are not. Names are shown in Title Case. " +
	"If the name is the same as their current one, nothing changes (status `unchanged`); say nothing changed. " +
	"If the name is already used by another member in any of their Spaces it is refused: ask for a different name. " +
	"Past records show the new name, because attribution uses the account's current name. " +
	"Space names (including their Personal Space label) are NOT renamed. " +
	"There is no undo except renaming again. Returns {status: renamed|unchanged, name}."

func registerAccountTools(server *mcpsdk.Server, registrar interfaceonboarding.ServiceOnboardingInterface) {
	addTool(server, &mcpsdk.Tool{
		Name:        "account_register",
		Description: "Ask for the user's name and birth date in YYYY-MM-DD format. Explain that the birth date is used for age-policy validation, show a summary, obtain explicit confirmation, then call with consent=true.",
	}, func(ctx context.Context, input AccountRegisterInput) (AccountRegisterOutput, error) {
		return AccountRegister(ctx, registrar, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name:        "account_link",
		Description: "Link the current WhatsApp number to an existing account without asking for a password or one-time code.",
	}, func(ctx context.Context, _ struct{}) (AccountRegisterOutput, error) {
		return AccountLink(ctx, registrar)
	})
	if registrar != nil {
		addTool(server, &mcpsdk.Tool{
			Name:        "account_rename",
			Description: accountRenameDescription,
		}, func(ctx context.Context, input AccountRenameInput) (AccountRenameOutput, error) {
			return AccountRename(ctx, registrar, input)
		})
	}
}
