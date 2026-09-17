package servicereset

import (
	"context"
	"errors"
	"family-assistant/pkg/config"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type resetRepoTestDouble struct {
	tokens           map[string]string
	cooldownTTL      time.Duration
	sendCount        int
	sendRetryAfter   time.Duration
	deletedHash      string
	clearedCooldown  string
	clearedSend      string
	setTokenErr      error
	getTokenErr      error
	setCooldownErr   error
	getCooldownErr   error
	incrementSendErr error
}

func newResetRepoTestDouble() *resetRepoTestDouble {
	return &resetRepoTestDouble{tokens: map[string]string{}}
}

func (m *resetRepoTestDouble) SetToken(ctx context.Context, hash, email string, ttl time.Duration) error {
	if m.setTokenErr != nil {
		return m.setTokenErr
	}
	m.tokens[hash] = email
	return nil
}
func (m *resetRepoTestDouble) GetEmailByToken(ctx context.Context, hash string) (string, error) {
	if m.getTokenErr != nil {
		return "", m.getTokenErr
	}
	email, ok := m.tokens[hash]
	if !ok {
		return "", redis.Nil
	}
	return email, nil
}
func (m *resetRepoTestDouble) DeleteToken(ctx context.Context, hash string) error {
	m.deletedHash = hash
	delete(m.tokens, hash)
	return nil
}
func (m *resetRepoTestDouble) SetCooldown(ctx context.Context, email string, ttl time.Duration) error {
	if m.setCooldownErr != nil {
		return m.setCooldownErr
	}
	return nil
}
func (m *resetRepoTestDouble) GetCooldownTTL(ctx context.Context, email string) (time.Duration, error) {
	if m.getCooldownErr != nil {
		return 0, m.getCooldownErr
	}
	return m.cooldownTTL, nil
}
func (m *resetRepoTestDouble) ClearCooldown(ctx context.Context, email string) error {
	m.clearedCooldown = email
	return nil
}
func (m *resetRepoTestDouble) IncrementSendCount(ctx context.Context, email string, ttl time.Duration) (int, time.Duration, error) {
	if m.incrementSendErr != nil {
		return 0, 0, m.incrementSendErr
	}
	m.sendCount++
	return m.sendCount, m.sendRetryAfter, nil
}
func (m *resetRepoTestDouble) ClearSendCount(ctx context.Context, email string) error {
	m.clearedSend = email
	return nil
}

type resetSenderTestDouble struct {
	to       string
	token    string
	appName  string
	resetURL string
	ttl      time.Duration
	err      error
}

func (m *resetSenderTestDouble) SendPasswordReset(to, token, appName, resetURL string, ttl time.Duration) error {
	m.to = to
	m.token = token
	m.appName = appName
	m.resetURL = resetURL
	m.ttl = ttl
	return m.err
}

func resetTestConfig() config.PasswordResetConfig {
	return config.PasswordResetConfig{
		TTL:         15 * time.Minute,
		Cooldown:    time.Minute,
		RateWindow:  time.Minute,
		RateLimit:   2,
		Secret:      "secret",
		URLTemplate: "https://example.com/reset?token={token}",
	}
}

func TestRequestResetStoresTokenAndSendsURL(t *testing.T) {
	repo := newResetRepoTestDouble()
	sender := &resetSenderTestDouble{}
	svc := NewPasswordResetService(repo, sender, resetTestConfig())

	if err := svc.RequestReset(context.Background(), " Jane.Doe@Example.COM ", "Starter"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if sender.to != "jane.doe@example.com" || sender.appName != "Starter" {
		t.Fatalf("unexpected sender call: %+v", sender)
	}
	if sender.token == "" || sender.resetURL != "https://example.com/reset?token="+sender.token {
		t.Fatalf("unexpected reset token/url: %+v", sender)
	}
	if repo.tokens[hashToken(sender.token, "secret")] != "jane.doe@example.com" {
		t.Fatalf("expected hashed token to be stored, got %+v", repo.tokens)
	}
}

func TestRequestResetReturnsThrottleOnRateLimit(t *testing.T) {
	repo := newResetRepoTestDouble()
	repo.sendCount = 2
	repo.sendRetryAfter = 45 * time.Second
	svc := NewPasswordResetService(repo, &resetSenderTestDouble{}, resetTestConfig())

	err := svc.RequestReset(context.Background(), "jane@example.com", "Starter")
	throttle, ok := errors.AsType[*ThrottleError](err)
	if !ok {
		t.Fatalf("expected throttle error, got %v", err)
	}
	if throttle.Reason != "rate_limit" || throttle.RetryAfter != 45*time.Second {
		t.Fatalf("unexpected throttle error: %+v", throttle)
	}
}

func TestRequestResetCleansUpWhenDeliveryFails(t *testing.T) {
	repo := newResetRepoTestDouble()
	svc := NewPasswordResetService(repo, &resetSenderTestDouble{err: errors.New("smtp down")}, resetTestConfig())

	err := svc.RequestReset(context.Background(), "jane@example.com", "Starter")
	if !errors.Is(err, ErrResetDeliveryFailed) {
		t.Fatalf("expected delivery failed error, got %v", err)
	}
	if repo.deletedHash == "" || repo.clearedCooldown != "jane@example.com" || repo.clearedSend != "jane@example.com" {
		t.Fatalf("expected cleanup for failed delivery, got %+v", repo)
	}
}

func TestVerifyResetReturnsEmailAndClearsState(t *testing.T) {
	repo := newResetRepoTestDouble()
	token := "reset-token"
	hash := hashToken(token, "secret")
	repo.tokens[hash] = "jane@example.com"
	svc := NewPasswordResetService(repo, nil, resetTestConfig())

	email, err := svc.VerifyReset(context.Background(), token)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if email != "jane@example.com" {
		t.Fatalf("expected email, got %q", email)
	}
	if repo.deletedHash != hash || repo.clearedCooldown != "jane@example.com" || repo.clearedSend != "jane@example.com" {
		t.Fatalf("expected reset state cleared, got %+v", repo)
	}
}

func TestBuildResetURLVariants(t *testing.T) {
	tests := map[string]string{
		"":                              "",
		"https://example.com/reset":     "https://example.com/reset?token=abc",
		"https://example.com/reset?a=1": "https://example.com/reset?a=1&token=abc",
		"https://example.com/{token}":   "https://example.com/abc",
	}

	for template, want := range tests {
		if got := buildResetURL(template, "abc"); got != want {
			t.Fatalf("template %q: expected %q, got %q", template, want, got)
		}
	}
}

func TestResetServiceNotConfigured(t *testing.T) {
	err := NewPasswordResetService(nil, nil, resetTestConfig()).RequestReset(context.Background(), "jane@example.com", "Starter")
	if !errors.Is(err, ErrResetNotConfigured) {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func TestRequestResetRejectsInvalidEmailAndCooldown(t *testing.T) {
	svc := NewPasswordResetService(newResetRepoTestDouble(), &resetSenderTestDouble{}, resetTestConfig())
	if err := svc.RequestReset(context.Background(), " ", "Starter"); !errors.Is(err, ErrResetInvalid) {
		t.Fatalf("expected invalid reset error, got %v", err)
	}

	repo := newResetRepoTestDouble()
	repo.cooldownTTL = 15 * time.Second
	svc = NewPasswordResetService(repo, &resetSenderTestDouble{}, resetTestConfig())
	err := svc.RequestReset(context.Background(), "jane@example.com", "Starter")
	throttle, ok := errors.AsType[*ThrottleError](err)
	if !ok {
		t.Fatalf("expected throttle error, got %v", err)
	}
	if throttle.Reason != "cooldown" || throttle.RetryAfter != 15*time.Second {
		t.Fatalf("unexpected throttle error: %+v", throttle)
	}
}

func TestRequestResetRepositoryErrors(t *testing.T) {
	tests := map[string]*resetRepoTestDouble{
		"cooldown":     {getCooldownErr: errors.New("redis down")},
		"rate limit":   {incrementSendErr: errors.New("redis down")},
		"store token":  {setTokenErr: errors.New("redis down")},
		"set cooldown": {setCooldownErr: errors.New("redis down")},
	}

	for name, repo := range tests {
		t.Run(name, func(t *testing.T) {
			if repo.tokens == nil {
				repo.tokens = map[string]string{}
			}
			svc := NewPasswordResetService(repo, &resetSenderTestDouble{}, resetTestConfig())
			if err := svc.RequestReset(context.Background(), "jane@example.com", "Starter"); err == nil {
				t.Fatal("expected repository error")
			}
		})
	}
}

func TestVerifyResetErrorBranches(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		_, err := NewPasswordResetService(nil, nil, resetTestConfig()).VerifyReset(context.Background(), "token")
		if !errors.Is(err, ErrResetNotConfigured) {
			t.Fatalf("expected not configured, got %v", err)
		}
	})
	t.Run("blank token", func(t *testing.T) {
		svc := NewPasswordResetService(newResetRepoTestDouble(), nil, resetTestConfig())
		_, err := svc.VerifyReset(context.Background(), " ")
		if !errors.Is(err, ErrResetInvalid) {
			t.Fatalf("expected invalid reset, got %v", err)
		}
	})
	t.Run("missing token", func(t *testing.T) {
		svc := NewPasswordResetService(newResetRepoTestDouble(), nil, resetTestConfig())
		_, err := svc.VerifyReset(context.Background(), "missing")
		if !errors.Is(err, ErrResetInvalid) {
			t.Fatalf("expected invalid reset, got %v", err)
		}
	})
	t.Run("get token error", func(t *testing.T) {
		repo := newResetRepoTestDouble()
		repo.getTokenErr = errors.New("redis down")
		svc := NewPasswordResetService(repo, nil, resetTestConfig())
		_, err := svc.VerifyReset(context.Background(), "token")
		if err == nil {
			t.Fatal("expected get token error")
		}
	})
}

func TestThrottleErrorString(t *testing.T) {
	var nilErr *ThrottleError
	if got := nilErr.Error(); got != "reset throttled" {
		t.Fatalf("expected nil throttle message, got %q", got)
	}

	err := &ThrottleError{Reason: "rate_limit"}
	if got := err.Error(); got != "reset throttled: rate_limit" {
		t.Fatalf("expected rate limit throttle message, got %q", got)
	}
}
