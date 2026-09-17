package serviceuser

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	domainuser "starter-kit/internal/domain/user"
	"starter-kit/internal/dto"
	interfacerole "starter-kit/internal/interfaces/role"
	"starter-kit/utils"
	"strings"
)

var (
	ErrGoogleNotConfigured        = errors.New("google login is not configured")
	ErrGoogleTokenInvalid         = errors.New("invalid google token")
	ErrGoogleEmailMissing         = errors.New("google account email is not available")
	ErrPublicRegistrationDisabled = errors.New("public registration is currently disabled")
)

func ValidatePasswordStrength(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters long")
	}

	hasLower := regexp.MustCompile(`[a-z]`).MatchString(password)
	if !hasLower {
		return errors.New("password must contain at least 1 lowercase letter (a-z)")
	}

	hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(password)
	if !hasUpper {
		return errors.New("password must contain at least 1 uppercase letter (A-Z)")
	}

	hasNumber := regexp.MustCompile(`[0-9]`).MatchString(password)
	if !hasNumber {
		return errors.New("password must contain at least 1 number (0-9)")
	}

	hasSymbol := regexp.MustCompile(`[^a-zA-Z0-9]`).MatchString(password)
	if !hasSymbol {
		return errors.New("password must contain at least 1 symbol (!@#$%^&*...)")
	}

	return nil
}

func buildUserAuthResponse(user domainuser.Users, permissions []string) map[string]any {
	if permissions == nil {
		permissions = []string{}
	}

	return map[string]any{
		"id":                  user.Id,
		"name":                user.Name,
		"email":               user.Email,
		"phone":               user.Phone,
		"role":                user.Role,
		"permissions":         permissions,
		"email_verified_at":   user.EmailVerifiedAt,
		"phone_verified_at":   user.PhoneVerifiedAt,
		"last_login_at":       user.LastLoginAt,
		"login_provider":      user.LoginProvider,
		"avatar_url":          user.AvatarURL,
		"password_changed_at": user.PasswordChangedAt,
		"created_at":          user.CreatedAt,
		"updated_at":          user.UpdatedAt,
	}
}

func findRoleIDByName(ctx context.Context, roleRepo interfacerole.RepoRoleInterface, roleName string) (*string, bool) {
	roleEntity, err := roleRepo.GetByName(ctx, roleName)
	if err != nil || roleEntity.Id == "" {
		return nil, false
	}

	return &roleEntity.Id, true
}

func ResolveLoginIdentifier(req dto.Login) (string, error) {
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Email)
	}

	if identifier == "" {
		return "", errors.New("identifier or email is required")
	}

	if strings.Contains(identifier, "@") {
		sanitizedEmail := utils.SanitizeEmail(identifier)
		if _, err := mail.ParseAddress(sanitizedEmail); err != nil {
			return "", errors.New("identifier must be a valid email or phone number")
		}
		return sanitizedEmail, nil
	}

	normalizedPhone := utils.NormalizePhoneTo62(identifier)
	if len(normalizedPhone) < 9 || len(normalizedPhone) > 15 {
		return "", errors.New("identifier must be a valid email or phone number")
	}

	return normalizedPhone, nil
}
