package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	domainidentity "family-assistant/internal/domain/identity"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/config"
)

type ActorContext = serviceidentity.ActorContext

type actorContextKey struct{}

type ExternalRequest struct {
	Provider   string
	ExternalID string
	Channel    string
}

type externalRequestContextKey struct{}

func WithActorContext(ctx context.Context, actor serviceidentity.ActorContext) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func ActorFromContext(ctx context.Context) (serviceidentity.ActorContext, bool) {
	if ctx == nil {
		return serviceidentity.ActorContext{}, false
	}
	actor, ok := ctx.Value(actorContextKey{}).(serviceidentity.ActorContext)
	return actor, ok
}

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

type TrustedExternalRequest = ExternalRequest

func WithTrustedExternalRequest(ctx context.Context, request ExternalRequest) context.Context {
	return WithExternalRequest(ctx, request)
}

func TrustedExternalRequestFromContext(ctx context.Context) (ExternalRequest, bool) {
	return ExternalRequestFromContext(ctx)
}

type AuthMiddleware struct {
	serverKey     string
	profileHeader string
}

func NewAuthMiddleware(cfg config.MCPConfig, _ ...any) *AuthMiddleware {
	profileHeader := strings.TrimSpace(cfg.ProfileHeader)
	if profileHeader == "" {
		profileHeader = "X-Hermes-Profile"
	}
	return &AuthMiddleware{
		serverKey:     strings.TrimSpace(cfg.ServerKey),
		profileHeader: profileHeader,
	}
}

func NewMiddleware(cfg config.MCPConfig, resolver ...any) *AuthMiddleware {
	return NewAuthMiddleware(cfg, resolver...)
}

func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedKey, ok := bearerSecret(r.Header.Get("Authorization"))
		if !ok || !constantTimeEqual(providedKey, m.serverKey) {
			writeHTTPError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		profileID := strings.TrimSpace(r.Header.Get(m.profileHeader))
		if profileID == "" {
			writeHTTPError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		channel := strings.TrimSpace(r.Header.Get("X-Hermes-Channel"))
		if channel == "" {
			channel = "whatsapp"
		}
		next.ServeHTTP(w, r.WithContext(WithExternalRequest(r.Context(), ExternalRequest{
			Provider: domainidentity.ProviderHermes, ExternalID: profileID, Channel: channel,
		})))
	})
}

type externalResolver interface {
	ResolveExternal(context.Context, string, string, string) (serviceidentity.ActorContext, error)
}

type legacyResolver interface {
	Resolve(context.Context, string, string) (serviceidentity.ActorContext, error)
}

func resolveActor(ctx context.Context, resolver any, externalID, channel string) (serviceidentity.ActorContext, error) {
	return resolveExternalActor(ctx, resolver, ExternalRequest{
		Provider: domainidentity.ProviderHermes, ExternalID: externalID, Channel: channel,
	})
}

func resolveExternalActor(ctx context.Context, resolver any, request ExternalRequest) (serviceidentity.ActorContext, error) {
	provider := strings.TrimSpace(request.Provider)
	if provider == "" {
		provider = domainidentity.ProviderHermes
	}
	externalID := strings.TrimSpace(request.ExternalID)
	channel := strings.TrimSpace(request.Channel)
	switch resolver := resolver.(type) {
	case *serviceidentity.ResolverService:
		if resolver == nil || resolver.IdentityRepo == nil {
			return resolver.Resolve(ctx, externalID, channel)
		}
		return resolver.ResolveExternal(ctx, provider, externalID, channel)
	case externalResolver:
		return resolver.ResolveExternal(ctx, provider, externalID, channel)
	case legacyResolver:
		return resolver.Resolve(ctx, externalID, channel)
	default:
		return serviceidentity.ActorContext{}, serviceidentity.ErrResolverMisconfigured
	}
}

func RequireActor(ctx context.Context, resolver any) (serviceidentity.ActorContext, error) {
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.ExternalID) == "" || strings.TrimSpace(request.Channel) == "" {
		return serviceidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	actor, err := resolveExternalActor(ctx, resolver, request)
	if err != nil {
		return serviceidentity.ActorContext{}, err
	}
	if strings.TrimSpace(actor.UserID) == "" {
		return serviceidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	return actor, nil
}

func bearerSecret(value string) (string, bool) {
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	secret := strings.TrimPrefix(value, "Bearer ")
	if secret == "" || strings.ContainsAny(secret, " \t\r\n") {
		return "", false
	}
	return secret, true
}

func constantTimeEqual(left, right string) bool {
	leftDigest := secretDigest(left)
	rightDigest := secretDigest(right)
	return subtle.ConstantTimeCompare(leftDigest[:], rightDigest[:]) == 1
}

func secretDigest(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}

func writeHTTPError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
