package interfacereset

import "context"

type ServicePasswordResetInterface interface {
	RequestReset(ctx context.Context, email, appName string) error
	VerifyReset(ctx context.Context, token string) (string, error)
}
