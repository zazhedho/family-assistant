package interfaceinvitation

import (
	"context"
	"time"

	domaininvitation "family-assistant/internal/domain/invitation"
)

type RepoInvitationInterface interface {
	Create(context.Context, *domaininvitation.Invitation) error
	Accept(context.Context, string, string, string, time.Time) (*domaininvitation.Acceptance, error)
}
