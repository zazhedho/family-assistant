package domaininvitation

import (
	"context"
	"time"
)

type Repository interface {
	Create(context.Context, *Invitation) error
	Accept(context.Context, string, string, string, time.Time) (*Acceptance, error)
}
