package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/config"
)

type ActorContext = serviceidentity.ActorContext

type actorContextKey struct{}

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

type AuthMiddleware struct {
	serverKey     string
	profileHeader string
	resolver      any
	logger        *slog.Logger
}

func NewAuthMiddleware(cfg config.MCPConfig, resolver any) *AuthMiddleware {
	profileHeader := strings.TrimSpace(cfg.ProfileHeader)
	if profileHeader == "" {
		profileHeader = "X-Hermes-Profile"
	}
	return &AuthMiddleware{
		serverKey:     strings.TrimSpace(cfg.ServerKey),
		profileHeader: profileHeader,
		resolver:      resolver,
		logger:        slog.Default(),
	}
}

func NewMiddleware(cfg config.MCPConfig, resolver any) *AuthMiddleware {
	return NewAuthMiddleware(cfg, resolver)
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
		actor, err := resolveActor(r.Context(), m.resolver, profileID, channel)
		if err != nil {
			if errors.Is(err, serviceidentity.ErrResolverMisconfigured) {
				m.logger.Error("mcp identity resolver is not configured")
				writeHTTPError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if errors.Is(err, serviceidentity.ErrUnauthenticated) {
				writeHTTPError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			m.logger.Error("mcp identity resolution failed", "error", err)
			writeHTTPError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithActorContext(r.Context(), actor)))
	})
}

type externalResolver interface {
	ResolveExternal(context.Context, string, string, string) (serviceidentity.ActorContext, error)
}

type legacyResolver interface {
	Resolve(context.Context, string, string) (serviceidentity.ActorContext, error)
}

func resolveActor(ctx context.Context, resolver any, externalID, channel string) (serviceidentity.ActorContext, error) {
	switch resolver := resolver.(type) {
	case externalResolver:
		return resolver.ResolveExternal(ctx, "hermes", externalID, channel)
	case legacyResolver:
		return resolver.Resolve(ctx, externalID, channel)
	default:
		return serviceidentity.ActorContext{}, serviceidentity.ErrResolverMisconfigured
	}
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
