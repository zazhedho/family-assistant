package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	serviceidentity "family-assistant/internal/services/identity"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	identityArgumentName    = "__hermes_identity"
	identityEnvelopeVersion = "v1"
	identityEnvelopeMaxAge  = 2 * time.Minute
	identityClockSkew       = 30 * time.Second
)

type IdentityEnvelope struct {
	Version    string `json:"version"`
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	Channel    string `json:"channel"`
	IssuedAt   int64  `json:"issued_at"`
	Nonce      string `json:"nonce"`
	Signature  string `json:"signature"`
}

type IdentityVerifier struct {
	secret     []byte
	maxAge     time.Duration
	now        func() time.Time
	nonceMu    sync.Mutex
	seenNonces map[string]time.Time
}

type identityVerifierContextKey struct{}

func NewIdentityVerifier(secret string) *IdentityVerifier {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	return &IdentityVerifier{
		secret: []byte(secret),
		maxAge: identityEnvelopeMaxAge,
		now:    time.Now,
	}
}

func withIdentityVerifier(ctx context.Context, verifier *IdentityVerifier) context.Context {
	return context.WithValue(ctx, identityVerifierContextKey{}, verifier)
}

func identityVerifierFromContext(ctx context.Context) *IdentityVerifier {
	if ctx == nil {
		return nil
	}
	verifier, _ := ctx.Value(identityVerifierContextKey{}).(*IdentityVerifier)
	return verifier
}

func toolIdentityContext(ctx context.Context, req *mcpsdk.CallToolRequest) (context.Context, error) {
	verifier := identityVerifierFromContext(ctx)
	if verifier == nil {
		return ctx, nil
	}
	request, err := verifier.Verify(req)
	if err != nil {
		return ctx, err
	}
	return WithExternalRequest(ctx, request), nil
}

func (v *IdentityVerifier) Verify(req *mcpsdk.CallToolRequest) (ExternalRequest, error) {
	if v == nil || len(v.secret) == 0 {
		return ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}
	if req == nil || req.Params == nil || len(req.Params.Arguments) == 0 {
		return ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}

	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(req.Params.Arguments, &arguments); err != nil {
		return ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}
	rawEnvelope, ok := arguments[identityArgumentName]
	if !ok {
		return ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}

	var envelope IdentityEnvelope
	if err := json.Unmarshal(rawEnvelope, &envelope); err != nil {
		return ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}
	if !v.valid(envelope) {
		return ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}
	now := time.Now()
	if v.now != nil {
		now = v.now()
	}
	if !v.consumeNonce(envelope.Nonce, now) {
		return ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}

	return ExternalRequest{
		Provider:   strings.ToLower(strings.TrimSpace(envelope.Provider)),
		ExternalID: strings.TrimSpace(envelope.ExternalID),
		Channel:    strings.ToLower(strings.TrimSpace(envelope.Channel)),
	}, nil
}

func (v *IdentityVerifier) consumeNonce(nonce string, now time.Time) bool {
	v.nonceMu.Lock()
	defer v.nonceMu.Unlock()

	if v.seenNonces == nil {
		v.seenNonces = make(map[string]time.Time)
	}
	for seen, expiresAt := range v.seenNonces {
		if !expiresAt.After(now) {
			delete(v.seenNonces, seen)
		}
	}
	if _, exists := v.seenNonces[nonce]; exists {
		return false
	}
	// ponytail: process-local replay cache; move it to Redis before horizontal scaling.
	v.seenNonces[nonce] = now.Add(v.maxAgeValue())
	return true
}

func (v *IdentityVerifier) valid(envelope IdentityEnvelope) bool {
	if envelope.Version != identityEnvelopeVersion || !safeIdentityPart(envelope.Provider) ||
		!safeIdentityPart(envelope.ExternalID) || !safeIdentityPart(envelope.Channel) ||
		!safeIdentityPart(envelope.Nonce) || envelope.IssuedAt <= 0 || envelope.Signature == "" {
		return false
	}
	if _, err := hex.DecodeString(envelope.Signature); err != nil {
		return false
	}
	now := time.Now()
	if v.now != nil {
		now = v.now()
	}
	issuedAt := time.Unix(envelope.IssuedAt, 0)
	if issuedAt.After(now.Add(identityClockSkew)) || now.Sub(issuedAt) > v.maxAgeValue() {
		return false
	}
	actual := signIdentityEnvelope(string(v.secret), envelope)
	return hmac.Equal([]byte(actual), []byte(strings.ToLower(envelope.Signature)))
}

func (v *IdentityVerifier) maxAgeValue() time.Duration {
	if v.maxAge > 0 {
		return v.maxAge
	}
	return identityEnvelopeMaxAge
}

func safeIdentityPart(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 512 && !strings.ContainsAny(value, "\x00\r\n")
}

func signIdentityEnvelope(secret string, envelope IdentityEnvelope) string {
	payload := strings.Join([]string{
		identityEnvelopeVersion,
		envelope.Provider,
		envelope.ExternalID,
		envelope.Channel,
		strconv.FormatInt(envelope.IssuedAt, 10),
		envelope.Nonce,
	}, "\x00")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
