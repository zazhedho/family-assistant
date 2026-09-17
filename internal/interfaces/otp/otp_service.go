package interfaceotp

import "context"

type ServiceOTPInterface interface {
	SendRegisterOTP(ctx context.Context, email, appName string) error
	VerifyRegisterOTP(ctx context.Context, email, code string) error
}
