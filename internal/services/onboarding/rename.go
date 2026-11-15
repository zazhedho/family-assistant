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

// Validation reasons for the `name` field. They are fixed server-written text (never the attempted
// name or the owner of a clashing name) and are shown to the bot by the MCP layer.
const (
	ReasonNameTaken      = "is already used by another member of your Spaces"
	ReasonNameLength     = "must contain 3 to 100 characters"
	ReasonNameCharacters = "contains characters that are not allowed"
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
	// Unchanged is decided on the STORED form (name is already NFC, collapsed and title-cased), so
	// adding or fixing accents on one's own name is a real rename. The key is only for the guard
	// against OTHER members; the caller's own account is excluded by the repository query.
	if name == norm.NFC.String(account.Name) {
		return dto.AccountRenameResult{Status: StatusUnchanged, Name: account.Name}, nil
	}
	key := accountNameKey(name)

	others, err := s.repository.OtherMemberNames(ctx, userID)
	if err != nil {
		return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, err)
	}
	for _, other := range others {
		if accountNameKey(other) == key {
			return dto.AccountRenameResult{}, s.failRename(ctx, channel, provider, userID, &serviceidentity.ValidationError{Field: "name", Reason: ReasonNameTaken})
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
// through. Format characters (Cf) include the zero width joiner and the tag characters, so ZWJ
// sequences (family, couple, rainbow flag) and tag-sequence flags are rejected in registration as
// well; plain country flags (regional indicator pairs) and single emoji are accepted.
func normalizeAccountName(raw string) (string, error) {
	invalid := &serviceidentity.ValidationError{Field: "name", Reason: ReasonNameCharacters}
	raw = strings.TrimSpace(raw)
	if !utf8.ValidString(raw) || strings.ContainsFunc(raw, isForbiddenNameRune) {
		return "", invalid
	}
	name := norm.NFC.String(utils.TitleCase(utils.StripHTML(raw)))
	if strings.ContainsFunc(name, isForbiddenNameRune) || strings.ContainsAny(name, "<>") {
		return "", invalid
	}
	if count := utf8.RuneCountInString(name); count < 3 || count > 100 {
		return "", &serviceidentity.ValidationError{Field: "name", Reason: ReasonNameLength}
	}
	return name, nil
}

// isForbiddenNameRune rejects control, format, private-use, surrogate and line/paragraph separator
// characters, plus the invisible fillers that have no legitimate use in a person's name and only
// serve to fake spaces. Variation selectors are deliberately allowed (U+FE0F is part of ordinary
// emoji); they are only dropped from the comparison key.
func isForbiddenNameRune(r rune) bool {
	switch r {
	case utf8.RuneError, 0x034f, 0x115f, 0x1160, 0x17b4, 0x17b5, 0x2800, 0x3164, 0xffa0:
		return true
	}
	if r >= 0x180b && r <= 0x180f {
		return true
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs, unicode.Zl, unicode.Zp)
}

// isInvisibleNameRune reports characters that render as nothing (Default_Ignorable_Code_Point plus
// the braille blank), so they must not influence a name comparison.
func isInvisibleNameRune(r rune) bool {
	switch r {
	case 0x034f, 0x115f, 0x1160, 0x17b4, 0x17b5, 0x2800, 0x3164, 0xffa0:
		return true
	}
	if r >= 0x180b && r <= 0x180f {
		return true
	}
	return unicode.In(r, unicode.Cf, unicode.Cc, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// accountNameKey is the identity of a display name for impersonation checks: compatibility
// normalization (full-width forms, NBSP and other spaces), invisible characters dropped, then
// decomposition with combining marks dropped after LATIN base letters and dotless i/j mapped to i/j,
// THEN spaces collapsed (so a stored "a <ZWSP> b" equals "a b"), case folded. It deliberately
// over-matches within Latin ("Jose" and "José", "Zoe" and "Zoë" are the same name for a household);
// marks after non-Latin bases (Arabic, Devanagari, Hebrew, Thai, ...) are kept so those names do not
// collapse (consequence: names differing only by Arabic harakat are not detected). Cross-script
// look-alikes (a Cyrillic letter for a Latin one) are not detected. Only the comparison uses the key;
// the stored name is never changed by it. Both sides of every comparison must go through it.
func accountNameKey(name string) string {
	var key strings.Builder
	afterLatin := false
	for _, r := range norm.NFD.String(strings.Map(func(r rune) rune {
		if isInvisibleNameRune(r) {
			return -1
		}
		return r
	}, norm.NFKC.String(name))) {
		if unicode.In(r, unicode.Mn, unicode.Me) {
			if !afterLatin {
				key.WriteRune(r)
			}
			continue
		}
		if !unicode.IsSpace(r) { // a mark stranded after a space still belongs to the preceding script
			afterLatin = unicode.Is(unicode.Latin, r)
		}
		switch r {
		case 0x0131:
			r = 'i'
		case 0x0237:
			r = 'j'
		}
		key.WriteRune(r)
	}
	return cases.Fold().String(strings.Join(strings.Fields(key.String()), " "))
}
