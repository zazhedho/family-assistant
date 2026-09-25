package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
)

const whatsappBridgeTimeout = 10 * time.Second

type WhatsAppBridge struct {
	endpoint string
	client   *http.Client
}

func NewWhatsAppBridge(baseURL string) (*WhatsAppBridge, error) {
	rawURL := strings.TrimSpace(baseURL)
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid WhatsApp bridge URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/send"
	return &WhatsAppBridge{
		endpoint: parsed.String(),
		client:   &http.Client{Timeout: whatsappBridgeTimeout},
	}, nil
}

func (b *WhatsAppBridge) Send(ctx context.Context, target domainreminder.DeliveryTarget, message string) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("WhatsApp bridge is not configured")
	}
	if strings.ToLower(strings.TrimSpace(target.Provider)) != "whatsapp" {
		return fmt.Errorf("unsupported notification provider %q", target.Provider)
	}
	if strings.TrimSpace(target.Target) == "" {
		return fmt.Errorf("notification target is required")
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("notification message is required")
	}
	payload, err := json.Marshal(struct {
		ChatID  string `json:"chatId"`
		Message string `json:"message"`
	}{ChatID: target.Target, Message: message})
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req) // #nosec G704 -- endpoint is operator-configured.
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("notification bridge status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
