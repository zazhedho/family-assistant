package mailer

import "time"

type PasswordResetSender interface {
	SendPasswordReset(to, token, appName, resetURL string, ttl time.Duration) error
}
