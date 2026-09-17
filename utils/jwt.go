package utils

import (
	"errors"
	"fmt"
	domainuser "starter-kit/internal/domain/user"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const minJWTKeyLength = 32

const (
	TokenTypeAccess        = "access"
	TokenTypeRefresh       = "refresh"
	TokenTypePasswordReset = "password_reset"
)

var (
	ErrJWTKeyNotConfigured = errors.New("jwt key is not configured")
	ErrJWTKeyTooShort      = fmt.Errorf("jwt key must be at least %d characters", minJWTKeyLength)
)

type AppClaims struct {
	UserId           string `json:"user_id"`
	Username         string `json:"username"`
	Role             string `json:"role"`
	TokenType        string `json:"token_type,omitempty"`
	IsImpersonated   bool   `json:"is_impersonated,omitempty"`
	OriginalUserId   string `json:"original_user_id,omitempty"`
	OriginalUsername string `json:"original_username,omitempty"`
	OriginalRole     string `json:"original_role,omitempty"`
	*jwt.RegisteredClaims
}

func GenerateJwt(user *domainuser.Users, logId string) (string, error) {
	return GenerateJwtWithClaims(user, logId, nil)
}

func GeneratePasswordResetJwt(user *domainuser.Users, logId string) (string, error) {
	resetExp := time.Now().Add(time.Duration(GetEnv("RESET_TTL_SECONDS", 900)) * time.Second)
	return GenerateJwtWithClaims(user, logId, &AppClaims{
		TokenType: TokenTypePasswordReset,
		RegisteredClaims: &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(resetExp),
		},
	})
}

func GenerateJwtWithClaims(user *domainuser.Users, logId string, claimsOverride *AppClaims) (string, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", err
	}

	accessExp := time.Now().Add(time.Hour * time.Duration(GetEnv("JWT_EXP", 24)))
	claims := AppClaims{
		UserId:    user.Id,
		Username:  user.Name,
		Role:      user.Role,
		TokenType: TokenTypeAccess,
		RegisteredClaims: &jwt.RegisteredClaims{
			ID:        logId,
			ExpiresAt: jwt.NewNumericDate(accessExp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	if claimsOverride != nil {
		if claimsOverride.TokenType != "" {
			claims.TokenType = claimsOverride.TokenType
		}
		claims.IsImpersonated = claimsOverride.IsImpersonated
		claims.OriginalUserId = claimsOverride.OriginalUserId
		claims.OriginalUsername = claimsOverride.OriginalUsername
		claims.OriginalRole = claimsOverride.OriginalRole
		if claimsOverride.RegisteredClaims != nil && claimsOverride.ExpiresAt != nil {
			claims.ExpiresAt = claimsOverride.ExpiresAt
		}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims)

	signedToken, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}

	return signedToken, nil
}

func GenerateRefreshJwt(user *domainuser.Users, logId string, claimsOverride *AppClaims) (string, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", err
	}

	refreshExp := time.Now().Add(time.Hour * time.Duration(GetEnv("REFRESH_TOKEN_EXP_HOURS", 168)))
	claims := &AppClaims{
		UserId:    user.Id,
		Username:  user.Name,
		Role:      user.Role,
		TokenType: TokenTypeRefresh,
		RegisteredClaims: &jwt.RegisteredClaims{
			ID:        logId,
			ExpiresAt: jwt.NewNumericDate(refreshExp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	if claimsOverride != nil {
		claims.IsImpersonated = claimsOverride.IsImpersonated
		claims.OriginalUserId = claimsOverride.OriginalUserId
		claims.OriginalUsername = claimsOverride.OriginalUsername
		claims.OriginalRole = claimsOverride.OriginalRole
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func GetAuthToken(ctx *gin.Context) string {
	bearerToken := ctx.Request.Header.Get("Authorization")
	return strings.ReplaceAll(bearerToken, "Bearer ", "")
}

func JwtClaims(ctx *gin.Context) (string, map[string]any, error) {
	tokenString := GetAuthToken(ctx)
	data, err := JwtClaim(tokenString)
	return tokenString, data, err
}

func JwtClaim(tokenString string) (map[string]any, error) {
	if tokenString == "" {
		return nil, errors.New("empty token")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		hmacSecret, err := jwtSecret()
		if err != nil {
			return nil, err
		}
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return "", fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return hmacSecret, nil
	})

	if err != nil || !token.Valid {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, err
}

func JwtExpiresAt(tokenString string) (time.Time, error) {
	claims, err := JwtClaim(tokenString)
	if err != nil {
		return time.Time{}, errors.New("invalid or expired token")
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return time.Time{}, errors.New("token expiry is required")
	}

	return time.Unix(int64(exp), 0), nil
}

func jwtSecret() ([]byte, error) {
	secret := GetEnv("JWT_KEY", "")
	if secret == "" {
		return nil, ErrJWTKeyNotConfigured
	}
	if len(secret) < minJWTKeyLength {
		return nil, ErrJWTKeyTooShort
	}
	return []byte(secret), nil
}

func ValidateJWTKeyConfigured() error {
	_, err := jwtSecret()
	return err
}
