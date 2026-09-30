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

func TestIdentityVerifierAcceptsSignedMessageTime(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier := &IdentityVerifier{secret: []byte("identity-secret"), now: func() time.Time { return now }}
	envelope := newSignedIdentityEnvelope("identity-secret", now)
	envelope.Version = "v2"
	envelope.MessageAt = now.Add(-10 * time.Minute).Unix()
	envelope.Signature = signIdentityEnvelope("identity-secret", envelope)
	if envelope.Signature != "f531954fea544f2ba8aa50901e448e837182ac4f68ef6ea5c4952c69836d5606" {
		t.Fatalf("v2 signature does not match Python wire format: %s", envelope.Signature)
	}
	request, err := verifier.Verify(toolRequestWithEnvelope(t, envelope))
	if err != nil || !request.MessageAt.Equal(time.Unix(envelope.MessageAt, 0)) {
		t.Fatalf("message time = %s, error = %v", request.MessageAt, err)
	}
}

func TestIdentityVerifierRejectsInvalidMessageTime(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, scenario := range []string{"tampered", "unsigned legacy", "missing", "future"} {
		t.Run(scenario, func(t *testing.T) {
			verifier := &IdentityVerifier{secret: []byte("identity-secret"), now: func() time.Time { return now }}
			envelope := newSignedIdentityEnvelope("identity-secret", now)
			envelope.Version, envelope.MessageAt = "v2", now.Add(-time.Minute).Unix()
			switch scenario {
			case "unsigned legacy":
				envelope.Version = "v1"
			case "missing":
				envelope.MessageAt = 0
			case "future":
				envelope.MessageAt = now.Add(time.Hour).Unix()
			}
			envelope.Signature = signIdentityEnvelope("identity-secret", envelope)
			if scenario == "tampered" {
				envelope.MessageAt--
			}
			if _, err := verifier.Verify(toolRequestWithEnvelope(t, envelope)); !errors.Is(err, serviceidentity.ErrUnauthenticated) {
				t.Fatalf("error = %v, want unauthenticated", err)
			}
		})
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
