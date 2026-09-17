package servicereset

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

func generateResetToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token, secret string) string {
	h := sha256.Sum256([]byte(token + secret))
	return hex.EncodeToString(h[:])
}

func buildResetURL(template, token string) string {
	template = strings.TrimSpace(template)
	if template == "" {
		return ""
	}
	if strings.Contains(template, "{token}") {
		return strings.ReplaceAll(template, "{token}", token)
	}
	if strings.Contains(template, "?") {
		return template + "&token=" + token
	}
	return template + "?token=" + token
}
