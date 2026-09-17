package serviceotp

import (
	"context"
	"errors"
	"fmt"
	interfaceotp "starter-kit/internal/interfaces/otp"
	"starter-kit/pkg/config"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/mailer"
	"starter-kit/utils"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrOTPInvalid        = errors.New("otp invalid or expired")
	ErrOTPTooManyAttempt = errors.New("otp too many attempts")
	ErrOTPNotConfigured  = errors.New("otp service not configured")
	ErrOTPDeliveryFailed = errors.New("otp delivery failed")
)

type ThrottleError struct {
	Reason     string
	RetryAfter time.Duration
}

func (e *ThrottleError) Error() string {
	if e == nil {
		return "otp throttled"
	}
	return fmt.Sprintf("otp throttled: %s", e.Reason)
}

type ServiceOTP struct {
	Repo   interfaceotp.RepoOTPInterface
	Sender mailer.Sender
	Config config.OTPConfig
}

func NewOTPService(repo interfaceotp.RepoOTPInterface, sender mailer.Sender, cfg config.OTPConfig) *ServiceOTP {
	return &ServiceOTP{Repo: repo, Sender: sender, Config: cfg}
}

func (s *ServiceOTP) SendRegisterOTP(ctx context.Context, email, appName string) error {
	if s == nil || s.Repo == nil || s.Sender == nil {
		return ErrOTPNotConfigured
	}

	normalizedEmail := utils.SanitizeEmail(email)
	if normalizedEmail == "" {
		return ErrOTPInvalid
	}

	cooldownTTL, err := s.Repo.GetCooldownTTL(ctx, normalizedEmail)
	if err != nil {
		return fmt.Errorf("check cooldown: %w", err)
	}
	if cooldownTTL > 0 {
		return &ThrottleError{Reason: "cooldown", RetryAfter: cooldownTTL}
	}

	if s.Config.RateLimit > 0 && s.Config.RateWindow > 0 {
		count, retryAfter, err := s.Repo.IncrementSendCount(ctx, normalizedEmail, s.Config.RateWindow)
		if err != nil {
			return fmt.Errorf("rate limit: %w", err)
		}
		if count > s.Config.RateLimit {
			return &ThrottleError{Reason: "rate_limit", RetryAfter: retryAfter}
		}
	}

	code, err := generateOTP()
	if err != nil {
		return fmt.Errorf("generate otp: %w", err)
	}

	hashed := hashOTP(code, s.Config.Secret)
	if err := s.Repo.SetOTP(ctx, normalizedEmail, hashed, s.Config.TTL); err != nil {
		_ = s.Repo.ClearSendCount(ctx, normalizedEmail)
		return fmt.Errorf("store otp: %w", err)
	}
	_ = s.Repo.ResetAttempts(ctx, normalizedEmail)
	if err := s.Repo.SetCooldown(ctx, normalizedEmail, s.Config.Cooldown); err != nil {
		_ = s.Repo.DeleteOTP(ctx, normalizedEmail)
		_ = s.Repo.ResetAttempts(ctx, normalizedEmail)
		_ = s.Repo.ClearSendCount(ctx, normalizedEmail)
		return fmt.Errorf("set cooldown: %w", err)
	}

	if err := s.Sender.SendOTP(normalizedEmail, code, appName); err != nil {
		_ = s.Repo.DeleteOTP(ctx, normalizedEmail)
		_ = s.Repo.ResetAttempts(ctx, normalizedEmail)
		_ = s.Repo.ClearCooldown(ctx, normalizedEmail)
		_ = s.Repo.ClearSendCount(ctx, normalizedEmail)
		logger.WriteLog(logger.LogLevelError, "OTP delivery error: ", err)
		return ErrOTPDeliveryFailed
	}

	return nil
}

func (s *ServiceOTP) VerifyRegisterOTP(ctx context.Context, email, code string) error {
	if s == nil || s.Repo == nil {
		return ErrOTPNotConfigured
	}

	normalizedEmail := utils.SanitizeEmail(email)
	cleanCode := strings.TrimSpace(code)
	if normalizedEmail == "" || cleanCode == "" {
		return ErrOTPInvalid
	}

	hashed, err := s.Repo.GetOTP(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrOTPInvalid
		}
		return fmt.Errorf("get otp: %w", err)
	}

	attempts, err := s.Repo.IncrementAttempts(ctx, normalizedEmail, s.Config.TTL)
	if err != nil {
		return fmt.Errorf("increment attempts: %w", err)
	}

	if s.Config.MaxAttempts > 0 && attempts > s.Config.MaxAttempts {
		_ = s.Repo.DeleteOTP(ctx, normalizedEmail)
		_ = s.Repo.ResetAttempts(ctx, normalizedEmail)
		return ErrOTPTooManyAttempt
	}

	if !verifyOTP(cleanCode, hashed, s.Config.Secret) {
		if s.Config.MaxAttempts > 0 && attempts >= s.Config.MaxAttempts {
			_ = s.Repo.DeleteOTP(ctx, normalizedEmail)
			_ = s.Repo.ResetAttempts(ctx, normalizedEmail)
			return ErrOTPTooManyAttempt
		}
		return ErrOTPInvalid
	}

	_ = s.Repo.DeleteOTP(ctx, normalizedEmail)
	_ = s.Repo.ResetAttempts(ctx, normalizedEmail)
	_ = s.Repo.ClearCooldown(ctx, normalizedEmail)
	_ = s.Repo.ClearSendCount(ctx, normalizedEmail)
	return nil
}

var _ interfaceotp.ServiceOTPInterface = (*ServiceOTP)(nil)
