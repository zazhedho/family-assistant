package mcp

import (
	"context"
	"strings"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	serviceidentity "family-assistant/internal/services/identity"
)

type ExternalRequest struct {
	Provider   string
	ExternalID string
	Channel    string
	ChatID     string
	ChatType   string
	MessageAt  time.Time
}

type externalRequestContextKey struct{}

func WithExternalRequest(ctx context.Context, request ExternalRequest) context.Context {
	return context.WithValue(ctx, externalRequestContextKey{}, request)
}

func ExternalRequestFromContext(ctx context.Context) (ExternalRequest, bool) {
	if ctx == nil {
		return ExternalRequest{}, false
	}
	request, ok := ctx.Value(externalRequestContextKey{}).(ExternalRequest)
	return request, ok
}

func resolveExternalActor(ctx context.Context, resolver interfaceidentity.Resolver, request ExternalRequest) (domainidentity.ActorContext, error) {
	provider := strings.TrimSpace(request.Provider)
	if provider == "" {
		provider = domainidentity.ProviderHermes
	}
	externalID := strings.TrimSpace(request.ExternalID)
	channel := strings.TrimSpace(request.Channel)
	if resolver == nil {
		return domainidentity.ActorContext{}, serviceidentity.ErrResolverMisconfigured
	}
	return resolver.ResolveExternal(ctx, provider, externalID, channel)
}

func RequireActor(ctx context.Context, resolver interfaceidentity.Resolver) (domainidentity.ActorContext, error) {
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ExternalID) == "" || strings.TrimSpace(request.Channel) == "" {
		return domainidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	actor, err := resolveExternalActor(ctx, resolver, request)
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	if strings.TrimSpace(actor.UserID) == "" {
		return domainidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	return actor, nil
}

func selectSpaceActor(ctx context.Context, resolver interfaceidentity.Resolver, selector string) (domainidentity.ActorContext, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	return serviceidentity.SelectSpace(ctx, actor, selector, selectorPermissions(resolver))
}

func selectorPermissions(resolver interfaceidentity.Resolver) interfaceidentity.PermissionLoader {
	if resolver, ok := resolver.(*serviceidentity.ResolverService); ok && resolver != nil {
		return resolver.PermissionService
	}
	if permissions, ok := resolver.(interfaceidentity.PermissionLoader); ok {
		return permissions
	}
	return nil
}
