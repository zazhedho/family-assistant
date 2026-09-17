package mailer

import (
	"starter-kit/utils"
	"strings"
	"testing"
	"time"
)

func TestNewBrevoSenderFromEnvValidatesRequiredConfig(t *testing.T) {
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PASS", "")
	t.Setenv("SMTP_FROM", "")

	if _, err := NewBrevoSenderFromEnv(); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestNewBrevoSenderFromEnvAppliesDefaults(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "bad-port")
	t.Setenv("SMTP_USER", "")
	t.Setenv("SMTP_PASS", "secret")
	t.Setenv("SMTP_FROM", "Starter <noreply@example.com>")
	t.Setenv("SMTP_SUBJECT", "")
	t.Setenv("RESET_SUBJECT", "")
	t.Setenv("AUTH_EMAIL_APP_NAME", "")
	t.Setenv("OTP_TTL", "7m")

	sender, err := NewBrevoSenderFromEnv()
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if sender.Port != defaultSMTPPort || sender.User != "apikey" || sender.Subject != "Your Registration OTP" {
		t.Fatalf("unexpected sender defaults: %+v", sender)
	}
	if sender.TTL != 7*time.Minute {
		t.Fatalf("expected TTL from env, got %v", sender.TTL)
	}
}

func TestBuildMailerMessagesEscapeHTMLAndIncludeFallbackMinutes(t *testing.T) {
	otp := string(buildOTPMessage("from@example.com", "to@example.com", "Subject", "<App>", "123456", 0))
	if !strings.Contains(otp, "&lt;App&gt;") || !strings.Contains(otp, "expires in 5 minutes") {
		t.Fatalf("unexpected otp message: %s", otp)
	}

	reset := string(buildPasswordResetMessage("from@example.com", "to@example.com", "Reset", "<App>", "token", "", 0))
	if !strings.Contains(reset, "&lt;App&gt;") || !strings.Contains(reset, "Reset token: token") || !strings.Contains(reset, "expires in 15 minutes") {
		t.Fatalf("unexpected reset message: %s", reset)
	}

	resetWithURL := string(buildPasswordResetMessage("from@example.com", "to@example.com", "Reset", "App", "token", "https://example.com/reset", time.Minute))
	if !strings.Contains(resetWithURL, "Reset Password") || !strings.Contains(resetWithURL, "https://example.com/reset") {
		t.Fatalf("unexpected reset URL message: %s", resetWithURL)
	}
}

func TestExtractEmailAndDurationFromEnv(t *testing.T) {
	if got := extractEmail("Starter <noreply@example.com>"); got != "noreply@example.com" {
		t.Fatalf("unexpected extracted email: %q", got)
	}
	if got := extractEmail("noreply@example.com"); got != "noreply@example.com" {
		t.Fatalf("unexpected plain email: %q", got)
	}

	t.Setenv("DURATION_A", "")
	t.Setenv("DURATION_B", "120")
	if got := utils.DurationFromEnv([]string{"DURATION_A", "DURATION_B"}, time.Minute); got != 120*time.Second {
		t.Fatalf("expected duration from seconds, got %v", got)
	}
}

func TestBrevoSenderSendMethodsReturnSMTPError(t *testing.T) {
	sender := &BrevoSender{
		Host:         "127.0.0.1",
		Port:         1,
		User:         "apikey",
		Pass:         "secret",
		From:         "Starter <noreply@example.com>",
		Subject:      "OTP",
		ResetSubject: "",
		TTL:          time.Minute,
		AppName:      "Starter",
	}

	if err := sender.SendOTP("to@example.com", "123456", ""); err == nil {
		t.Fatal("expected otp smtp error")
	}
	if err := sender.SendPasswordReset("to@example.com", "token", "", "", 0); err == nil {
		t.Fatal("expected reset smtp error")
	}
}
