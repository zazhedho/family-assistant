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
)

const (
	StatusRenamed   = "renamed"
	StatusUnchanged = "unchanged"

	auditResourceUser = "user"
)

// Rename changes the display name of the account linked to the caller's signed external identity.
// The target account is never an input: it is resolved from the identity alone.
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
	if currentName := strings.Join(strings.Fields(account.Name), " "); strings.EqualFold(name, currentName) {
		return dto.AccountRenameResult{Status: StatusUnchanged, Name: account.Name}, nil
	}

	taken, err := s.repository.NameTakenInSharedSpaces(ctx, userID, name)
	if err != nil {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, err)
	}
	if taken {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, &serviceidentity.ValidationError{Field: "name", Reason: "is already used by another member of your Spaces"})
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
// form, for registration and rename alike. Forbidden characters are rejected both before and after
// HTML stripping so entity-encoded control or invisible characters cannot slip through.
func normalizeAccountName(raw string) (string, error) {
	invalid := &serviceidentity.ValidationError{Field: "name", Reason: "contains characters that are not allowed"}
	raw = strings.TrimSpace(raw)
	if !utf8.ValidString(raw) || strings.ContainsFunc(raw, isForbiddenNameRune) {
		return "", invalid
	}
	name := utils.TitleCase(utils.StripHTML(raw))
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
