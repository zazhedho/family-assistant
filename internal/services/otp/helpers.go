package serviceotp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
)

func generateOTP() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func hashOTP(code, secret string) string {
	h := sha256.Sum256([]byte(code + secret))
	return hex.EncodeToString(h[:])
}

func verifyOTP(code, hashed, secret string) bool {
	candidate := hashOTP(code, secret)
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(hashed)) == 1
}
