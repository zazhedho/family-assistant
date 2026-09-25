package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainreminder "family-assistant/internal/domain/reminder"
)

func TestWhatsAppBridgeSendsChatMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/send" {
			t.Fatalf("request = %s %s, want POST /send", r.Method, r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["chatId"] != "120363@g.us" || payload["message"] != "⏰ Pengingat: susu bayi" {
			t.Fatalf("payload = %#v", payload)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender, err := NewWhatsAppBridge(server.URL)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if err := sender.Send(context.Background(), domainreminder.DeliveryTarget{Provider: "whatsapp", Target: "120363@g.us"}, "⏰ Pengingat: susu bayi"); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestWhatsAppBridgeRejectsUnsupportedProvider(t *testing.T) {
	sender, err := NewWhatsAppBridge("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if err := sender.Send(context.Background(), domainreminder.DeliveryTarget{Provider: "telegram", Target: "chat-1"}, "hello"); err == nil || !strings.Contains(err.Error(), "unsupported notification provider") {
		t.Fatalf("error = %v, want unsupported provider", err)
	}
}

func TestWhatsAppBridgeReturnsNonSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bridge offline", http.StatusBadGateway)
	}))
	defer server.Close()

	sender, err := NewWhatsAppBridge(server.URL)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	err = sender.Send(context.Background(), domainreminder.DeliveryTarget{Provider: "whatsapp", Target: "chat-1"}, "hello")
	if err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error = %v, want bridge status", err)
	}
}
