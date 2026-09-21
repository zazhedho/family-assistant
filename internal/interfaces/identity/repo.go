package interfaceidentity

import (
	"context"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
)

type RepoIdentityInterface interface {
	CreateLinkToken(context.Context, *domainidentity.LinkToken) error
	ConsumeAndLink(context.Context, string, string, string, time.Time) (*domainidentity.ExternalIdentity, error)
	FindActive(context.Context, string, string) (*domainidentity.ExternalIdentity, error)
	Revoke(context.Context, string, string, string) error
}
