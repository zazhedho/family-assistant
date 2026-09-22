package interfaceinvitation

import (
	"context"
	"time"

	domaininvitation "family-assistant/internal/domain/invitation"
)

type RepoInvitationInterface interface {
	Create(context.Context, *domaininvitation.Invitation) error
	Accept(context.Context, string, string, string, time.Time) (*domaininvitation.Acceptance, error)
	ListPending(context.Context, string) ([]domaininvitation.Invitation, error)
	RevokePending(context.Context, string, string, time.Time) error
}
