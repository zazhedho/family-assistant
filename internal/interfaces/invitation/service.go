package interfaceinvitation

import (
	"context"

	domaininvitation "family-assistant/internal/domain/invitation"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	"family-assistant/internal/dto"
)

type ServiceInvitationInterface interface {
	Create(context.Context, string, dto.InvitationCreateInput) (*domaininvitation.Invitation, string, error)
	Accept(context.Context, string, domainuser.Users) (*domainspace.Member, error)
}
