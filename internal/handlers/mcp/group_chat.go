package mcp

import (
	"context"
	"strings"

	domainidentity "family-assistant/internal/domain/identity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	serviceidentity "family-assistant/internal/services/identity"
)

// A group, channel, thread or guild chat shares one LLM session across its
// participants, so anything a tool returns there is visible to all of them. These
// helpers rely only on the HMAC-verified request; unverified header mode never
// counts as a shared chat.

const errCodeDMRequired = "dm_required"

type groupSpaceLookupContextKey struct{}

func withGroupSpaceLookup(ctx context.Context, lookup interfaceidentity.GroupSpaceLookup) context.Context {
	if lookup == nil {
		return ctx
	}
	return context.WithValue(ctx, groupSpaceLookupContextKey{}, lookup)
}

func groupSpaceLookupFromContext(ctx context.Context) interfaceidentity.GroupSpaceLookup {
	if ctx == nil {
		return nil
	}
	lookup, _ := ctx.Value(groupSpaceLookupContextKey{}).(interfaceidentity.GroupSpaceLookup)
	return lookup
}

// isVerifiedSharedChat reports whether the HMAC-verified request comes from a chat
// that may be shared by several people. Hermes chat types are dm, group, thread,
// guild and channel (plus webhook), so every non-DM type counts as shared
// regardless of channel. An empty chat type (legacy envelopes, DMs) is not shared.
func isVerifiedSharedChat(ctx context.Context) (ExternalRequest, bool) {
	request, ok := verifiedExternalRequestFromContext(ctx)
	if !ok {
		return ExternalRequest{}, false
	}
	return request, request.ChatType != "" && request.ChatType != "dm"
}

// requireDirectMessage refuses tools whose input or output must not enter a shared group session.
func requireDirectMessage(ctx context.Context) error {
	if _, shared := isVerifiedSharedChat(ctx); shared {
		return &MCPError{Code: errCodeDMRequired, Message: "this action must be done in a private chat with the assistant"}
	}
	return nil
}

func errGroupSpaceRequired() error {
	return &MCPError{Code: "invalid_input", Message: "space is required in a group chat that is not bound to a Space; ask the user which Space to use"}
}

// effectiveSpaceSelector keeps explicit selectors and DM behavior unchanged. In a
// verified shared chat a blank selector defaults to the Space bound to that chat,
// never to the sender's Personal Space. Without a chat ID or a binding it fails
// closed.
func effectiveSpaceSelector(ctx context.Context, selector string) (string, error) {
	if strings.TrimSpace(selector) != "" {
		return selector, nil
	}
	request, shared := isVerifiedSharedChat(ctx)
	if !shared {
		return selector, nil
	}
	if request.ChatID == "" {
		return "", errGroupSpaceRequired()
	}
	lookup := groupSpaceLookupFromContext(ctx)
	if lookup == nil {
		return "", errGroupSpaceRequired()
	}
	spaceID, found, err := lookup.ActiveSpaceIDForChat(ctx, request.Channel, request.ChatID)
	if err != nil {
		return "", err
	}
	if !found || strings.TrimSpace(spaceID) == "" {
		return "", errGroupSpaceRequired()
	}
	return spaceID, nil
}

// selectActorSpace is the single Space selection path for every tool: it applies
// the group default, then the normal membership and permission check.
func selectActorSpace(ctx context.Context, resolver interfaceidentity.Resolver, actor domainidentity.ActorContext, selector string) (domainidentity.ActorContext, error) {
	selector, err := effectiveSpaceSelector(ctx, selector)
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	return serviceidentity.SelectSpace(ctx, actor, selector, selectorPermissions(resolver))
}
