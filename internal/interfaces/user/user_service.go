package interfaceuser

import (
	"context"
	domainuser "starter-kit/internal/domain/user"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
)

type ServiceUserInterface interface {
	RegisterUser(ctx context.Context, req dto.UserRegister) (domainuser.Users, error)
	AdminCreateUser(ctx context.Context, req dto.AdminCreateUser) (domainuser.Users, error)
	LoginUser(ctx context.Context, req dto.Login, logId string, metadata dto.LoginMetadata) (string, error)
	LoginWithGoogle(ctx context.Context, req dto.GoogleLogin, metadata dto.LoginMetadata, allowRegistration bool) (domainuser.Users, bool, error)
	LogoutUser(ctx context.Context, token string) error
	ImpersonateUser(ctx context.Context, targetUserId string, logId string) (string, error)
	StopImpersonation(ctx context.Context, logId string) (string, error)
	GetUserById(ctx context.Context, id string) (domainuser.Users, error)
	GetUserByEmail(ctx context.Context, email string) (domainuser.Users, error)
	GetUserByPhone(ctx context.Context, phone string) (domainuser.Users, error)
	GetUserByAuth(ctx context.Context, id string) (map[string]any, error)
	GetAllUsers(ctx context.Context, params filter.BaseParams) ([]domainuser.Users, int64, error)
	Update(ctx context.Context, id string, req dto.UserUpdate) (domainuser.Users, error)
	ChangePassword(ctx context.Context, id string, req dto.ChangePassword) (domainuser.Users, error)
	ForgotPassword(ctx context.Context, req dto.ForgotPasswordRequest) (string, error)
	ResetPassword(ctx context.Context, req dto.ResetPasswordRequest) error
	ResetPasswordByEmail(ctx context.Context, email, newPassword string) error
	Delete(ctx context.Context, id string) error
}
