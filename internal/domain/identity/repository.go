package domainidentity

import (
	"context"
	"time"
)

type Repository interface {
	CreateLinkToken(context.Context, *LinkToken) error
	ConsumeAndLink(context.Context, string, string, string, time.Time) (*ExternalIdentity, error)
	FindActive(context.Context, string, string) (*ExternalIdentity, error)
	Revoke(context.Context, string, string, string) error
}
