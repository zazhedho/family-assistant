package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	serviceidentity "family-assistant/internal/services/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIdentityVerifierAcceptsValidEnvelopeAndOverridesHeaderIdentity(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier := &IdentityVerifier{
		secret: []byte("identity-secret"),
		maxAge: time.Minute,
		now:    func() time.Time { return now },
	}
	envelope := newSignedIdentityEnvelope("identity-secret", now)
	ctx := WithExternalRequest(context.Background(), ExternalRequest{
		Provider:   "hermes",
		ExternalID: "stale-profile",
		Channel:    "whatsapp",
	})
	ctx = withIdentityVerifier(ctx, verifier)

	gotContext, err := toolIdentityContext(ctx, toolRequestWithEnvelope(t, envelope))
	if err != nil {
		t.Fatalf("tool identity context: %v", err)
	}
	got, ok := ExternalRequestFromContext(gotContext)
	if !ok {
		t.Fatal("expected external request in context")
	}
	if got.Provider != "hermes" || got.ExternalID != "6285333320090@s.whatsapp.net" || got.Channel != "whatsapp" {
		t.Fatalf("unexpected trusted identity: %+v", got)
	}
	if got.ChatID != "120363@g.us" || got.ChatType != "group" {
		t.Fatalf("unexpected chat context: %+v", got)
	}
}

func TestIdentityVerifierRejectsMissingEnvelope(t *testing.T) {
	verifier := &IdentityVerifier{
		secret: []byte("identity-secret"),
		maxAge: time.Minute,
		now:    func() time.Time { return time.Unix(1_800_000_000, 0) },
	}
	_, err := verifier.Verify(&mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{}})
	if !errors.Is(err, serviceidentity.ErrUnauthenticated) {
		t.Fatalf("error = %v, want unauthenticated", err)
	}
}

func TestIdentityVerifierRejectsTamperedEnvelope(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier := &IdentityVerifier{
		secret: []byte("identity-secret"),
		maxAge: time.Minute,
		now:    func() time.Time { return now },
	}
	envelope := newSignedIdentityEnvelope("identity-secret", now)
	envelope.ExternalID = "attacker@s.whatsapp.net"

	_, err := verifier.Verify(toolRequestWithEnvelope(t, envelope))
	if !errors.Is(err, serviceidentity.ErrUnauthenticated) {
		t.Fatalf("error = %v, want unauthenticated", err)
	}
}

func TestIdentityVerifierRejectsTamperedChatContext(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier := &IdentityVerifier{
		secret: []byte("identity-secret"),
		maxAge: time.Minute,
		now:    func() time.Time { return now },
	}
	envelope := newSignedIdentityEnvelope("identity-secret", now)
	envelope.ChatID = "attacker@g.us"

	_, err := verifier.Verify(toolRequestWithEnvelope(t, envelope))
	if !errors.Is(err, serviceidentity.ErrUnauthenticated) {
		t.Fatalf("error = %v, want unauthenticated", err)
	}
}

func TestIdentityVerifierRejectsExpiredEnvelope(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier := &IdentityVerifier{
		secret: []byte("identity-secret"),
		maxAge: time.Minute,
		now:    func() time.Time { return now },
	}
	envelope := newSignedIdentityEnvelope("identity-secret", now.Add(-2*time.Minute))

	_, err := verifier.Verify(toolRequestWithEnvelope(t, envelope))
	if !errors.Is(err, serviceidentity.ErrUnauthenticated) {
		t.Fatalf("error = %v, want unauthenticated", err)
	}
}

func TestIdentityVerifierRejectsReplayedEnvelope(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier := &IdentityVerifier{
		secret: []byte("identity-secret"),
		maxAge: time.Minute,
		now:    func() time.Time { return now },
	}
	envelope := newSignedIdentityEnvelope("identity-secret", now)
	request := toolRequestWithEnvelope(t, envelope)
	if _, err := verifier.Verify(request); err != nil {
		t.Fatalf("first verification: %v", err)
	}

	_, err := verifier.Verify(request)
	if !errors.Is(err, serviceidentity.ErrUnauthenticated) {
		t.Fatalf("replay error = %v, want unauthenticated", err)
	}
}

func TestIdentityToolContextKeepsLegacyHeaderWhenVerifierDisabled(t *testing.T) {
	ctx := WithExternalRequest(context.Background(), ExternalRequest{
		Provider:   "hermes",
		ExternalID: "profile-1",
		Channel:    "whatsapp",
	})

	gotContext, err := toolIdentityContext(ctx, &mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{}})
	if err != nil {
		t.Fatalf("tool identity context: %v", err)
	}
	got, ok := ExternalRequestFromContext(gotContext)
	if !ok || got.ExternalID != "profile-1" {
		t.Fatalf("legacy identity was not preserved: %+v, %v", got, ok)
	}
}

func toolRequestWithEnvelope(t *testing.T, envelope IdentityEnvelope) *mcpsdk.CallToolRequest {
	t.Helper()
	arguments, err := json.Marshal(map[string]any{identityArgumentName: envelope})
	if err != nil {
		t.Fatalf("marshal identity envelope: %v", err)
	}
	return &mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{Arguments: arguments}}
}

func newSignedIdentityEnvelope(secret string, issuedAt time.Time) IdentityEnvelope {
	envelope := IdentityEnvelope{
		Version:    identityEnvelopeVersion,
		Provider:   "hermes",
		ExternalID: "6285333320090@s.whatsapp.net",
		Channel:    "whatsapp",
		ChatID:     "120363@g.us",
		ChatType:   "group",
		IssuedAt:   issuedAt.Unix(),
		Nonce:      "nonce-1",
	}
	envelope.Signature = signIdentityEnvelope(secret, envelope)
	return envelope
}
