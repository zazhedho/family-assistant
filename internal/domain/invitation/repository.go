package domaininvitation

import (
	"context"
	"time"

	domainspace "family-assistant/internal/domain/space"
)

type Repository interface {
	Create(context.Context, *Invitation) error
	Accept(context.Context, string, string, string, time.Time) (*domainspace.Member, error)
}
