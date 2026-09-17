package dto

type UserRegister struct {
	Name          string `json:"name" binding:"required,min=3,max=100"`
	Email         string `json:"email" binding:"required,email"`
	Phone         string `json:"phone" binding:"required,min=9,max=15"`
	Password      string `json:"password" binding:"required,min=8,max=64"`
	OTPCode       string `json:"otp_code" binding:"omitempty,len=6,numeric"`
	EmailVerified bool   `json:"-"`
}

type AdminCreateUser struct {
	Name     string `json:"name" binding:"required,min=3,max=100"`
	Email    string `json:"email" binding:"required,email"`
	Phone    string `json:"phone" binding:"omitempty,min=9,max=15"`
	Password string `json:"password" binding:"required,min=8,max=64"`
	Role     string `json:"role" binding:"required"`
}

type Login struct {
	Identifier string `json:"identifier" binding:"omitempty,min=3,max=100"`
	Email      string `json:"email" binding:"omitempty,min=3,max=100"`
	Password   string `json:"password" binding:"required,min=8,max=64"`
}

type LoginMetadata struct {
	IP        string
	UserAgent string
}

type GoogleLogin struct {
	IDToken string `json:"id_token" binding:"required"`
}

type UserUpdate struct {
	Name  string `json:"name" binding:"omitempty,min=3,max=100"`
	Email string `json:"email" binding:"omitempty,email"`
	Phone string `json:"phone" binding:"omitempty,min=9,max=15"`
	Role  string `json:"role" binding:"omitempty"`
}

type ChangePassword struct {
	CurrentPassword string `json:"current_password" binding:"required,min=8,max=64"`
	NewPassword     string `json:"new_password" binding:"required,min=8,max=64"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=64"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type SendRegisterOTPRequest struct {
	Email string `json:"email" binding:"required,email"`
	Phone string `json:"phone" binding:"omitempty,min=9,max=15"`
}
