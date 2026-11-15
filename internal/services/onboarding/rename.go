package serviceonboarding

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/utils"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	StatusRenamed   = "renamed"
	StatusUnchanged = "unchanged"

	auditResourceUser = "user"
)

// Rename changes the display name of the account linked to the caller's signed external identity.
// The target account is never an input: it is resolved from the identity alone.
//
// Concurrent renames of two members to the same NEW name are possible because no unique constraint
// exists; they cannot impersonate an already existing name, which is checked beforehand.
func (s *Service) Rename(ctx context.Context, input dto.AccountRenameInput) (dto.AccountRenameResult, error) {
	provider := domainidentity.NormalizeProvider(input.Provider)
	externalID := strings.TrimSpace(input.ExternalID)
	channel := strings.TrimSpace(input.Channel)
	if provider == "" || externalID == "" {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, "", serviceidentity.ErrUnauthenticated)
	}
	if s.repository == nil {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, "", errors.New("onboarding repository is not configured"))
	}

	account, err := s.repository.FindAccountForRename(ctx, provider, externalID)
	if err != nil {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, "", err)
	}
	userID := strings.TrimSpace(account.ID)
	if userID == "" {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, "", domainidentity.ErrIdentityNotFound)
	}

	name, err := normalizeAccountName(input.Name)
	if err != nil {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, err)
	}
	key := accountNameKey(name)
	if key == accountNameKey(account.Name) {
		return dto.AccountRenameResult{Status: StatusUnchanged, Name: account.Name}, nil
	}

	others, err := s.repository.OtherMemberNames(ctx, userID)
	if err != nil {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, err)
	}
	for _, other := range others {
		if accountNameKey(other) == key {
			return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, &serviceidentity.ValidationError{Field: "name", Reason: "is already used by another member of your Spaces"})
		}
	}

	now := s.now().UTC()
	if err := s.repository.UpdateUserName(ctx, userID, name, now); err != nil {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, err)
	}

	event := s.auditEvent(channel, provider, domainaudit.StatusSuccess, userID)
	event.Action = domainaudit.ActionUpdate
	event.Resource = auditResourceUser
	event.BeforeData = map[string]any{"name": account.Name}
	event.AfterData = map[string]any{"name": name}
	s.store(ctx, event)
	return dto.AccountRenameResult{Status: StatusRenamed, Name: name}, nil
}

// failRename audits a rejected rename. The event carries only a fixed category and
// the caller's own user id; the attempted name is never recorded.
func (s *Service) failRename(ctx context.Context, channel, provider, userID string, err error) error {
	event := s.auditEvent(channel, provider, domainaudit.StatusFailed, userID)
	event.Action = domainaudit.ActionUpdate
	event.Resource = auditResourceUser
	event.ErrorMessage = failureCategory(err)
	s.store(ctx, event)
	return err
}

// normalizeAccountName is the single place that turns a requested display name into the stored
// form (NFC, collapsed spaces), for registration and rename alike. Forbidden characters are rejected
// both before and after HTML stripping so entity-encoded control or invisible characters cannot slip
// through. Format characters (Cf) include the zero width joiner, so ZWJ emoji and flag sequences are
// rejected in registration as well.
func normalizeAccountName(raw string) (string, error) {
	invalid := &serviceidentity.ValidationError{Field: "name", Reason: "contains characters that are not allowed"}
	raw = strings.TrimSpace(raw)
	if !utf8.ValidString(raw) || strings.ContainsFunc(raw, isForbiddenNameRune) {
		return "", invalid
	}
	name := norm.NFC.String(utils.TitleCase(utils.StripHTML(raw)))
	if strings.ContainsFunc(name, isForbiddenNameRune) || strings.ContainsAny(name, "<>") {
		return "", invalid
	}
	if count := utf8.RuneCountInString(name); count < 3 || count > 100 {
		return "", &serviceidentity.ValidationError{Field: "name", Reason: "must contain 3 to 100 characters"}
	}
	return name, nil
}

func isForbiddenNameRune(r rune) bool {
	return r == utf8.RuneError || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs, unicode.Zl, unicode.Zp)
}

// accountNameKey is the identity of a display name for impersonation checks: compatibility
// normalization (full-width forms, NBSP and other spaces), collapsed spaces, no format or control
// characters, case folded. Both sides of every comparison must go through it.
func accountNameKey(name string) string {
	name = strings.Join(strings.Fields(norm.NFKC.String(name)), " ")
	name = strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Cf, unicode.Cc) {
			return -1
		}
		return r
	}, name)
	return cases.Fold().String(name)
}
