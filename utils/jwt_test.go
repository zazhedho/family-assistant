package utils

import (
	"errors"
	"net/http/httptest"
	domainuser "starter-kit/internal/domain/user"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateJwtIncludesAccessClaims(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	token, err := GenerateJwt(&domainuser.Users{
		Id:   "user-1",
		Name: "Jane",
		Role: RoleViewer,
	}, "log-1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	claims, err := JwtClaim(token)
	if err != nil {
		t.Fatalf("expected valid token, got %v", err)
	}

	if claims["user_id"] != "user-1" || claims["token_type"] != "access" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestGenerateRefreshJwtIncludesRefreshTokenType(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	token, err := GenerateRefreshJwt(&domainuser.Users{
		Id:   "user-1",
		Name: "Jane",
		Role: RoleViewer,
	}, "log-1", nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	claims, err := JwtClaim(token)
	if err != nil {
		t.Fatalf("expected valid token, got %v", err)
	}
	if claims["token_type"] != "refresh" {
		t.Fatalf("expected refresh token type, got %+v", claims)
	}
}

func TestGeneratePasswordResetJwtIncludesDedicatedTokenType(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")
	t.Setenv("RESET_TTL_SECONDS", "900")

	token, err := GeneratePasswordResetJwt(&domainuser.Users{Id: "user-1"}, "reset-1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	claims, err := JwtClaim(token)
	if err != nil {
		t.Fatalf("expected valid token, got %v", err)
	}
	if claims["token_type"] != TokenTypePasswordReset {
		t.Fatalf("expected password reset token type, got %+v", claims)
	}
}

func TestJwtExpiresAt(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	token, err := GenerateJwt(&domainuser.Users{
		Id:   "user-1",
		Name: "Jane",
		Role: RoleViewer,
	}, "log-1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	expiresAt, err := JwtExpiresAt(token)
	if err != nil {
		t.Fatalf("expected token expiry, got %v", err)
	}
	if expiresAt.IsZero() || time.Until(expiresAt) <= 0 {
		t.Fatalf("expected future token expiry, got %v", expiresAt)
	}
}

func TestJwtExpiresAtRequiresExpClaim(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": "user-1"})
	tokenString, err := token.SignedString([]byte("test-secret-must-be-at-least-32-bytes"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	if _, err := JwtExpiresAt(tokenString); err == nil || err.Error() != "token expiry is required" {
		t.Fatalf("expected token expiry error, got %v", err)
	}
}

func TestJwtClaimRejectsUnexpectedSigningMethod(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"user_id": "user-1"})
	tokenString, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to sign none token: %v", err)
	}

	if _, err := JwtClaim(tokenString); err == nil {
		t.Fatal("expected signing method error")
	}
}

func TestJWTRequiresConfiguredSecret(t *testing.T) {
	t.Setenv("JWT_KEY", "")
	user := &domainuser.Users{
		Id:   "user-1",
		Name: "Jane",
		Role: RoleViewer,
	}

	if _, err := GenerateJwt(user, "log-1"); !errors.Is(err, ErrJWTKeyNotConfigured) {
		t.Fatalf("expected jwt key error for access token, got %v", err)
	}
	if _, err := GenerateRefreshJwt(user, "log-1", nil); !errors.Is(err, ErrJWTKeyNotConfigured) {
		t.Fatalf("expected jwt key error for refresh token, got %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": "user-1"})
	tokenString, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	if _, err := JwtClaim(tokenString); !errors.Is(err, ErrJWTKeyNotConfigured) {
		t.Fatalf("expected jwt key error for token parsing, got %v", err)
	}
}

func TestJWTRejectsWhitespaceOnlySecret(t *testing.T) {
	t.Setenv("JWT_KEY", "   ")

	_, err := GenerateJwt(&domainuser.Users{Id: "user-1"}, "log-1")
	if !errors.Is(err, ErrJWTKeyNotConfigured) {
		t.Fatalf("expected jwt key error, got %v", err)
	}
	if err := ValidateJWTKeyConfigured(); !errors.Is(err, ErrJWTKeyNotConfigured) {
		t.Fatalf("expected startup validation error, got %v", err)
	}
}

func TestJWTRejectsShortSecret(t *testing.T) {
	t.Setenv("JWT_KEY", "a")

	_, err := GenerateJwt(&domainuser.Users{Id: "user-1"}, "log-1")
	if !errors.Is(err, ErrJWTKeyTooShort) {
		t.Fatalf("expected short jwt key error, got %v", err)
	}
	if err := ValidateJWTKeyConfigured(); !errors.Is(err, ErrJWTKeyTooShort) {
		t.Fatalf("expected startup validation short key error, got %v", err)
	}
}

func TestValidateJWTKeyConfiguredAcceptsNonEmptySecret(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	if err := ValidateJWTKeyConfigured(); err != nil {
		t.Fatalf("expected valid jwt key, got %v", err)
	}
}

func TestGetAuthTokenStripsBearerPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	ctx.Request.Header.Set("Authorization", "Bearer abc.def.ghi")

	if got := GetAuthToken(ctx); got != "abc.def.ghi" {
		t.Fatalf("expected stripped bearer token, got %q", got)
	}
}
