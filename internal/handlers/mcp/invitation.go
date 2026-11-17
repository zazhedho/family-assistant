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
	"family-assistant/utils"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type InvitationCreateInput struct {
	Space        string `json:"space,omitempty" jsonschema:"shared Space UUID or exact name"`
	RoleName     string `json:"role_name" jsonschema:"space_admin, space_member, space_editor, or space_viewer"`
	InvitedEmail string `json:"invited_email,omitempty" jsonschema:"optional email; leave blank for a WhatsApp bearer invitation"`
}

type InvitationCreateOutput struct {
	Invitation *domaininvitation.Invitation `json:"invitation"`
	Token      string                       `json:"token"`
}

type MemberAddByPhoneInput struct {
	Space       string `json:"space,omitempty" jsonschema:"the shared Space's UUID or exact name; in a private chat ALWAYS pass it, because blank selects the sender's Personal Space which is never a shared Space and fails with not found"`
	Phone       string `json:"phone" jsonschema:"the person's WhatsApp number exactly as the owner gave it, for example 0812-3456-7890 or 6281234567890; never guess or complete a number"`
	RoleName    string `json:"role_name,omitempty" jsonschema:"space_admin, space_member, space_editor, or space_viewer; blank means space_member"`
	ConfirmName string `json:"confirm_name,omitempty" jsonschema:"leave blank on the first call; on the second call set it to exactly the display_name the preview returned, only after the owner said yes"`
}

type MemberAddByPhoneOutput struct {
	Status      string `json:"status" jsonschema:"needs_confirmation (nobody was added yet) or added"`
	DisplayName string `json:"display_name"`
	SpaceName   string `json:"space_name"`
	RoleName    string `json:"role_name"`
	PhoneTail   string `json:"phone_tail" jsonschema:"three dots plus the last three digits of the number"`
}

const (
	memberAddStatusPreview = "needs_confirmation"
	memberAddStatusAdded   = "added"
	memberAddDefaultRole   = "space_member"
	memberAddTailDigits    = 3

	memberAddDescription = "Add a person who is ALREADY REGISTERED to a shared Space, immediately and without any invitation to accept, by their WhatsApp number. " +
		"Only an owner or admin of the Space may do this, and only in a private chat (DM) with the assistant, never in a group. " +
		"The person must already be registered: they register by chatting with the bot in a private chat, after the owner approved them in Hermes. " +
		"Two steps, always: (1) FIRST call WITHOUT confirm_name; nobody is added, you only get back whose number it is (display_name), the space_name, the role_name and the last three digits. " +
		"(2) SHOW the returned name, Space and role to the owner and ask for an explicit yes. A human must see this preview: never chain the two calls without the owner's confirmation in between. " +
		"Only after the owner says yes, call again with confirm_name set to EXACTLY that display_name. " +
		"Never guess or complete a phone number; use only what the owner typed. " +
		"The person is added with the chosen role_name (default space_member). " +
		"In a private chat ALWAYS pass space (the shared Space's name or UUID): leaving it blank selects the sender's Personal Space, which is never a shared Space, and the call then fails with 'not found'. A 'not found' with a blank space does NOT mean the person is unregistered. " +
		"On a 'not found' error with space set, tell the owner that the person must DM the bot and complete registration first, or that the number is wrong. " +
		"On a 'conflict' error tell the owner the person is already a member of this Space. " +
		"For someone who has no account yet, use invitation_create (a bearer invitation) instead."
)

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
	// The bearer token returned here would be usable by any group member.
	if err := requireDirectMessage(ctx); err != nil {
		return InvitationCreateOutput{}, err
	}
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return InvitationCreateOutput{}, MapToolError(err)
	}
	if service == nil {
		return InvitationCreateOutput{}, MapToolError(errors.New("invitation service is not configured"))
	}
	selected, err := selectActorSpace(ctx, resolver, actor, input.Space)
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

// MemberAddByPhone previews or performs an add by verified phone. The phone and
// confirm_name only travel to the service: the output carries the name and the
// last three digits, never the number.
func MemberAddByPhone(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceinvitation.ServiceInvitationInterface, input MemberAddByPhoneInput) (MemberAddByPhoneOutput, error) {
	// The preview reveals a person's name; keep it out of shared group sessions.
	if err := requireDirectMessage(ctx); err != nil {
		return MemberAddByPhoneOutput{}, err
	}
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return MemberAddByPhoneOutput{}, MapToolError(err)
	}
	if service == nil {
		return MemberAddByPhoneOutput{}, MapToolError(errors.New("invitation service is not configured"))
	}
	selected, err := selectActorSpace(ctx, resolver, actor, input.Space)
	if err != nil {
		return MemberAddByPhoneOutput{}, MapToolError(err)
	}
	roleName := strings.TrimSpace(input.RoleName)
	if roleName == "" {
		roleName = memberAddDefaultRole
	}
	result, err := service.AddMemberByPhone(ctx, actor.UserID, dto.MemberAddByPhoneInput{
		SpaceID: selected.SpaceID, Phone: input.Phone, RoleName: roleName, ConfirmName: input.ConfirmName,
	})
	if err != nil {
		return MemberAddByPhoneOutput{}, MapToolError(err)
	}
	if result == nil {
		return MemberAddByPhoneOutput{}, MapToolError(errors.New("invitation service returned no member add result"))
	}
	status := memberAddStatusPreview
	if result.Added {
		status = memberAddStatusAdded
	}
	return MemberAddByPhoneOutput{
		Status: status, DisplayName: result.Preview.DisplayName, SpaceName: result.Preview.SpaceName,
		RoleName: result.Preview.RoleName, PhoneTail: maskedPhoneTail(result.Preview.PhoneTail),
	}, nil
}

// maskedPhoneTail renders "..." plus at most the last three digits.
func maskedPhoneTail(tail string) string {
	runes := []rune(tail)
	if len(runes) > memberAddTailDigits {
		runes = runes[len(runes)-memberAddTailDigits:]
	}
	return "..." + string(runes)
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
	selected, err := selectActorSpace(ctx, resolver, actor, input.Space)
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
	selected, err := selectActorSpace(ctx, resolver, actor, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	invitationID, err := utils.ParseUUID(input.InvitationID, "invitation_id", true)
	if err != nil {
		return nil, MapToolError(err)
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
	}, func(ctx context.Context, input InvitationCreateInput) (InvitationCreateOutput, error) {
		return InvitationCreate(ctx, resolver, service, input)
	})
	if service != nil {
		addTool(server, &mcpsdk.Tool{
			Name: "member_add_by_phone", Description: memberAddDescription,
		}, func(ctx context.Context, input MemberAddByPhoneInput) (MemberAddByPhoneOutput, error) {
			return MemberAddByPhone(ctx, resolver, service, input)
		})
	}
	addTool(server, &mcpsdk.Tool{
		Name: "invitation_accept", Description: "Accept a one-time Space invitation token for the authenticated WhatsApp account.",
	}, func(ctx context.Context, input InvitationAcceptInput) (*domainspace.Member, error) {
		return InvitationAccept(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "invitation_list", Description: "List pending invitations for an authorized shared Space without exposing invitation tokens.",
	}, func(ctx context.Context, input InvitationListInput) (InvitationListOutput, error) {
		output, err := InvitationList(ctx, resolver, service, input)
		return InvitationListOutput{Invitations: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "invitation_revoke", Description: "Revoke a pending invitation in an authorized shared Space.",
	}, func(ctx context.Context, input InvitationRevokeInput) (*domaininvitation.Invitation, error) {
		return InvitationRevoke(ctx, resolver, service, input)
	})
}
