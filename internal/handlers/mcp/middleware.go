package mcp

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/pkg/config"
	"family-assistant/pkg/logger"
	"family-assistant/utils"
)

type AuthMiddleware struct {
	serverKey     string
	profileHeader string
	identity      *IdentityVerifier
}

func NewAuthMiddleware(cfg config.MCPConfig) *AuthMiddleware {
	profileHeader := strings.TrimSpace(cfg.ProfileHeader)
	if profileHeader == "" {
		profileHeader = "X-Hermes-Profile"
	}
	return &AuthMiddleware{
		serverKey:     strings.TrimSpace(cfg.ServerKey),
		profileHeader: profileHeader,
		identity:      NewIdentityVerifier(cfg.IdentitySecret),
	}
}

func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedKey, ok := bearerSecret(r.Header.Get("Authorization"))
		if !ok || !constantTimeEqual(providedKey, m.serverKey) {
			writeHTTPError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		ctx := r.Context()
		logID := logger.LogIDFromContext(ctx)
		if logID == "" {
			logID = utils.CreateUUID()
		}
		ctx = logger.WithLogMetadata(ctx, logID, "")

		profileID := strings.TrimSpace(r.Header.Get(m.profileHeader))
		channel := strings.TrimSpace(r.Header.Get("X-Hermes-Channel"))
		if channel == "" {
			channel = "whatsapp"
		}
		// MCP connection setup (initialize/tools/list) has no chat sender context.
		// Tool handlers enforce a non-empty external identity before account or
		// protected operations, so an authenticated setup request may proceed
		// without this per-message header.
		ctx = WithExternalRequest(ctx, ExternalRequest{
			Provider: domainidentity.ProviderHermes, ExternalID: profileID, Channel: channel,
		})
		ctx = withIdentityVerifier(ctx, m.identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
