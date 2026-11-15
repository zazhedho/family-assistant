package serviceonboarding

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"
	"family-assistant/internal/dto"
	serviceidentity "family-assistant/internal/services/identity"
)

func TestNormalizeAccountName(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		invalid bool
	}{
		{name: "title cases", raw: "onty zeia", want: "Onty Zeia"},
		{name: "single word", raw: "rheina", want: "Rheina"},
		{name: "trims and collapses spaces", raw: "  Rheina   Putri ", want: "Rheina Putri"},
		{name: "unicode", raw: " josé álvarez ", want: "José Álvarez"},
		{name: "html stripped", raw: " <b>jane doe</b> ", want: "Jane Doe"},
		{name: "html leaves too short", raw: "<b>x</b>", invalid: true},
		{name: "too short", raw: "ab", invalid: true},
		{name: "only spaces", raw: "     ", invalid: true},
		{name: "empty", raw: "", invalid: true},
		{name: "too long", raw: strings.Repeat("a", 101), invalid: true},
		{name: "max length ok", raw: strings.Repeat("a", 100), want: "A" + strings.Repeat("a", 99)},
		{name: "newline inside", raw: "Jane\nDoe", invalid: true},
		{name: "tab inside", raw: "Jane\tDoe", invalid: true},
		{name: "carriage return inside", raw: "Jane\rDoe", invalid: true},
		{name: "null byte", raw: "Jane\x00Doe", invalid: true},
		{name: "zero width space", raw: "Daddy\u200bZeia", invalid: true},
		{name: "zero width joiner", raw: "Daddy\u200dZeia", invalid: true},
		{name: "bom", raw: "\xef\xbb\xbfJane Doe", invalid: true},
		{name: "right to left override", raw: "Jane\u202eDoe", invalid: true},
		{name: "line separator", raw: "Jane\u2028Doe", invalid: true},
		{name: "entity encoded zero width", raw: "Daddy&#8203;Zeia", invalid: true},
		{name: "entity encoded newline collapses to a space", raw: "Jane&#10;Doe", want: "Jane Doe"},
		{name: "lone angle bracket", raw: "Jane < Doe", invalid: true},
		{name: "entity encoded angle bracket", raw: "Jane &lt;Doe", invalid: true},
		{name: "invalid utf8", raw: "Jane\xffDoe", invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAccountName(tt.raw)
			if tt.invalid {
				var validation *serviceidentity.ValidationError
				if !errors.As(err, &validation) || validation.Field != "name" || got != "" {
					t.Fatalf("got %q, err %v, want ValidationError on name", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, err %v, want %q", got, err, tt.want)
			}
		})
	}
}

func renameInput(name string) dto.AccountRenameInput {
	return dto.AccountRenameInput{Name: name, Provider: " HERMES ", ExternalID: " profile-1 ", Channel: " whatsapp "}
}

func renameHarness(t *testing.T) *serviceHarness {
	t.Helper()
	h := newServiceHarness(t)
	h.repository.renameUser = domainonboarding.UserRef{ID: "user-1", Name: "Onty Zeia"}
	return h
}

func assertNoWrites(t *testing.T, h *serviceHarness) {
	t.Helper()
	if h.repository.updateCalls != 0 {
		t.Fatalf("update calls = %d, want 0", h.repository.updateCalls)
	}
}

func TestRenameWritesOnceAndAuditsBeforeAfter(t *testing.T) {
	h := renameHarness(t)
	got, err := h.service.Rename(context.Background(), renameInput("  rheina   putri "))
	if err != nil || got != (dto.AccountRenameResult{Status: StatusRenamed, Name: "Rheina Putri"}) {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
	if h.repository.updateCalls != 1 || h.repository.updatedID != "user-1" || h.repository.updatedName != "Rheina Putri" {
		t.Fatalf("update = %d %q %q", h.repository.updateCalls, h.repository.updatedID, h.repository.updatedName)
	}
	if h.repository.namesUserID != "user-1" {
		t.Fatalf("other member names requested for %q", h.repository.namesUserID)
	}
	if len(h.audit.events) != 1 {
		t.Fatalf("audit events = %#v", h.audit.events)
	}
	event := h.audit.events[0]
	if event.Action != domainaudit.ActionUpdate || event.Resource != "user" || event.ResourceID != "user-1" || event.ActorUserID != "user-1" ||
		event.Status != domainaudit.StatusSuccess || event.Channel != "whatsapp" {
		t.Fatalf("event = %#v", event)
	}
	if event.Metadata["provider"] != "hermes" || event.Metadata["channel"] != "whatsapp" || len(event.Metadata) != 2 {
		t.Fatalf("metadata = %#v", event.Metadata)
	}
	before, _ := event.BeforeData.(map[string]any)
	after, _ := event.AfterData.(map[string]any)
	if before["name"] != "Onty Zeia" || after["name"] != "Rheina Putri" {
		t.Fatalf("before/after = %#v / %#v", event.BeforeData, event.AfterData)
	}
}

func TestRenameUnchangedIsNoOp(t *testing.T) {
	for _, name := range []string{"Onty Zeia", "onty zeia", "  ONTY   zeia "} {
		h := renameHarness(t)
		got, err := h.service.Rename(context.Background(), renameInput(name))
		if err != nil || got != (dto.AccountRenameResult{Status: StatusUnchanged, Name: "Onty Zeia"}) {
			t.Fatalf("%q: result = %+v, err = %v", name, got, err)
		}
		assertNoWrites(t, h)
		if len(h.audit.events) != 0 {
			t.Fatalf("%q: audit events = %#v", name, h.audit.events)
		}
	}
}

func TestRenameUnlinkedIdentityIsRejected(t *testing.T) {
	h := newServiceHarness(t)
	_, err := h.service.Rename(context.Background(), renameInput("Rheina Putri"))
	if !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		t.Fatalf("err = %v, want ErrIdentityNotFound", err)
	}
	assertNoWrites(t, h)
	if h.repository.namesCalls != 0 {
		t.Fatalf("collision checked for unlinked caller")
	}
	if len(h.audit.events) != 1 || h.audit.events[0].Status != domainaudit.StatusFailed || h.audit.events[0].ErrorMessage != "not_linked" {
		t.Fatalf("audit = %#v", h.audit.events)
	}
}

func TestRenameRequiresTrustedIdentity(t *testing.T) {
	for _, input := range []dto.AccountRenameInput{
		{Name: "Rheina Putri", Provider: "", ExternalID: "profile-1"},
		{Name: "Rheina Putri", Provider: "hermes", ExternalID: "  "},
	} {
		h := renameHarness(t)
		_, err := h.service.Rename(context.Background(), input)
		if !errors.Is(err, serviceidentity.ErrUnauthenticated) || h.repository.renameFinds != 0 {
			t.Fatalf("err = %v, finds = %d", err, h.repository.renameFinds)
		}
		assertNoWrites(t, h)
	}
}

func TestRenameRejectsInvalidNamesWithoutWriting(t *testing.T) {
	for _, name := range []string{"ab", "   ", "Jane\nDoe", "Daddy\u200bZeia", "<b>x</b>", strings.Repeat("a", 101)} {
		h := renameHarness(t)
		_, err := h.service.Rename(context.Background(), renameInput(name))
		var validation *serviceidentity.ValidationError
		if !errors.As(err, &validation) || validation.Field != "name" {
			t.Fatalf("%q: err = %v", name, err)
		}
		assertNoWrites(t, h)
	}
}

const collisionReason = ReasonNameTaken

func sharedNames() []string {
	return []string{
		"Daddy Zeia",
		"Jos\u00e9 Zeia",        // NFC
		"Zoe\u0308 Putri",       // NFD
		"Mommy\u00adZeia",       // stored with a soft hyphen
		"Double   Spaced  Name", // stored with repeated spaces
		"Fullwidth Kid",
	}
}

func TestRenameRejectsAdversarialSpellingsOfSharedMemberNames(t *testing.T) {
	attempts := []string{
		"Daddy Zeia", "daddy zeia", "DADDY ZEIA", "  Daddy    Zeia  ", "Daddy\u00a0Zeia", "Daddy\u2003Zeia", "Daddy\u3000Zeia",
		"\uff24\uff41\uff44\uff44\uff59 \uff3a\uff45\uff49\uff41",
		"Jos\u00e9 Zeia", "Jose\u0301 Zeia", "JOSE\u0301 ZEIA",
		"Zo\u00eb Putri", "Zoe\u0308 Putri", "zo\u00cb putri",
		"MommyZeia", "Daddy Zeia\ufe0f", "Daddy\ufe0f Zeia", "Daddy Zeia\ufe00",
		"Double Spaced Name", "double  spaced   name",
	}
	for _, attempt := range attempts {
		h := renameHarness(t)
		h.repository.otherNames = sharedNames()
		_, err := h.service.Rename(context.Background(), renameInput(attempt))
		var validation *serviceidentity.ValidationError
		if !errors.As(err, &validation) || validation.Field != "name" || validation.Reason != collisionReason || h.repository.namesCalls != 1 {
			t.Fatalf("%+q: err = %v, names calls = %d, want the collision error", attempt, err, h.repository.namesCalls)
		}
		if strings.Contains(strings.ToLower(validation.Reason), "daddy") {
			t.Fatalf("reason echoes name: %q", validation.Reason)
		}
		assertNoWrites(t, h)
	}
}

func TestRenameRejectsInsertedZeroWidthBeforeComparison(t *testing.T) {
	h := renameHarness(t)
	h.repository.otherNames = sharedNames()
	_, err := h.service.Rename(context.Background(), renameInput("Daddy\u200bZeia"))
	var validation *serviceidentity.ValidationError
	if !errors.As(err, &validation) || validation.Field != "name" || h.repository.namesCalls != 0 {
		t.Fatalf("err = %v, names calls = %d", err, h.repository.namesCalls)
	}
	assertNoWrites(t, h)
}

func TestRenameAllowsNamesNotUsedInSharedSpaces(t *testing.T) {
	for _, name := range []string{"Daddy Zeiah", "Rheina Putri", "Fullwidth"} {
		h := renameHarness(t)
		h.repository.otherNames = sharedNames()
		got, err := h.service.Rename(context.Background(), renameInput(name))
		if err != nil || got.Status != StatusRenamed || h.repository.updateCalls != 1 {
			t.Fatalf("%q: result = %+v, err = %v, updates = %d", name, got, err, h.repository.updateCalls)
		}
	}
	// a name held by an account that is NOT returned (not in a shared Space) is allowed
	h := renameHarness(t)
	h.repository.otherNames = []string{"Somebody Else"}
	if got, err := h.service.Rename(context.Background(), renameInput("Daddy Zeia")); err != nil || got.Status != StatusRenamed {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
}

func TestRenameStoresNFCWithCollapsedSpaces(t *testing.T) {
	h := renameHarness(t)
	got, err := h.service.Rename(context.Background(), renameInput("  jose\u0301   garcia\u00a0 "))
	if err != nil || h.repository.updatedName != "Jos\u00e9 Garcia" || got.Name != h.repository.updatedName {
		t.Fatalf("result = %+v, err = %v, stored %+q", got, err, h.repository.updatedName)
	}
}

func TestRenameSameNameInOtherFormIsUnchanged(t *testing.T) {
	h := renameHarness(t)
	h.repository.renameUser = domainonboarding.UserRef{ID: "user-1", Name: "Jos\u00e9 Zeia"}
	for _, name := range []string{"Jose\u0301 Zeia", "JOS\u00c9  ZEIA", "jos\u00e9\u00a0zeia"} {
		got, err := h.service.Rename(context.Background(), renameInput(name))
		if err != nil || got.Status != StatusUnchanged {
			t.Fatalf("%+q: result = %+v, err = %v", name, got, err)
		}
	}
	assertNoWrites(t, h)
}

func TestAccountNameKeyIgnoresFormattingAndCase(t *testing.T) {
	want := accountNameKey("Daddy Zeia")
	for _, v := range []string{"DADDY ZEIA", " daddy\u3000zeia ", "\uff24\uff41\uff44\uff44\uff59 \uff3a\uff45\uff49\uff41", "Dad\u200bdy  Zeia"} {
		if accountNameKey(v) != want {
			t.Fatalf("%+q key = %q, want %q", v, accountNameKey(v), want)
		}
	}
	if accountNameKey("Jose\u0301") != accountNameKey("Jos\u00e9") {
		t.Fatal("NFD and NFC keys differ")
	}
	if accountNameKey("Daddy Zeia") == accountNameKey("Daddy Zeiah") {
		t.Fatal("different names share a key")
	}
}

func TestRenameCollisionCheckErrorBlocksWrite(t *testing.T) {
	h := renameHarness(t)
	h.repository.namesErr = errors.New("db down")
	if _, err := h.service.Rename(context.Background(), renameInput("Rheina Putri")); err == nil {
		t.Fatal("expected error")
	}
	assertNoWrites(t, h)
}

func TestRenameUpdateErrorIsReportedAndNotAuditedAsSuccess(t *testing.T) {
	h := renameHarness(t)
	h.repository.updateErr = errors.New("boom")
	if _, err := h.service.Rename(context.Background(), renameInput("Rheina Putri")); err == nil {
		t.Fatal("expected error")
	}
	if len(h.audit.events) != 1 || h.audit.events[0].Status != domainaudit.StatusFailed || h.audit.events[0].ErrorMessage != "internal" {
		t.Fatalf("audit = %#v", h.audit.events)
	}
}

func TestRenameFailureAuditNeverContainsAttemptedName(t *testing.T) {
	attempted := "Secret Attempt Name"
	cases := map[string]func(*serviceHarness){
		"collision": func(h *serviceHarness) { h.repository.otherNames = []string{"secret attempt name"} },
		"update":    func(h *serviceHarness) { h.repository.updateErr = errors.New("write failed for " + attempted) },
		"taken":     func(h *serviceHarness) { h.repository.namesErr = errors.New("check failed for " + attempted) },
	}
	for label, setup := range cases {
		h := renameHarness(t)
		setup(h)
		_, _ = h.service.Rename(context.Background(), renameInput(attempted))
		if len(h.audit.events) != 1 || h.audit.events[0].Status != domainaudit.StatusFailed {
			t.Fatalf("%s: audit = %#v", label, h.audit.events)
		}
		encoded, err := json.Marshal(h.audit.events[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, leaked := range []string{attempted, "Secret", "secret attempt"} {
			if strings.Contains(string(encoded), leaked) {
				t.Fatalf("%s: audit leaks %q: %s", label, leaked, encoded)
			}
		}
		if h.audit.events[0].BeforeData != nil || h.audit.events[0].AfterData != nil {
			t.Fatalf("%s: failure audit carries names: %s", label, encoded)
		}
	}
	// invalid name rejected before lookup of collisions
	h := renameHarness(t)
	_, _ = h.service.Rename(context.Background(), renameInput("Secret\nName"))
	encoded, _ := json.Marshal(h.audit.events)
	if strings.Contains(string(encoded), "Secret") {
		t.Fatalf("validation audit leaks name: %s", encoded)
	}
}

func TestRenameWithoutRepositoryFailsClosed(t *testing.T) {
	service := NewService(nil, nil, nil)
	if _, err := service.Rename(context.Background(), renameInput("Rheina Putri")); err == nil {
		t.Fatal("expected error with nil repository")
	}
}

func TestRegisterSharesNameRulesWithRename(t *testing.T) {
	h := newServiceHarness(t)
	input := validInput()
	input.Name = "Jane\u200bDoe"
	_, err := h.service.Register(context.Background(), input)
	var validation *serviceidentity.ValidationError
	if !errors.As(err, &validation) || validation.Field != "name" || h.repository.createCalls != 0 {
		t.Fatalf("err = %v, creates = %d", err, h.repository.createCalls)
	}
}

func TestRegisterStoresNFCName(t *testing.T) {
	h := newServiceHarness(t)
	input := validInput()
	input.Name = "jose\u0301 alvarez"
	if _, err := h.service.Register(context.Background(), input); err != nil || h.repository.created.User.Name != "Jos\u00e9 Alvarez" {
		t.Fatalf("name = %+q, err = %v", h.repository.created.User.Name, err)
	}
}

func TestNormalizeAccountNameKeepsValidSpecialNames(t *testing.T) {
	for raw, want := range map[string]string{"rheina\U0001f98b": "Rheina\U0001f98b", "o'neil": "O'neil", "\u00f1andu\u0301": "\u00d1and\u00fa", "mcdonald": "Mcdonald", "rheina\u2764\ufe0f": "Rheina\u2764\ufe0f", "plane\u2708\ufe0f x": "Plane\u2708\ufe0f X"} {
		got, err := normalizeAccountName(raw)
		if err != nil || got != want {
			t.Fatalf("%+q: got %+q, err %v, want %+q", raw, got, err, want)
		}
	}
}

func TestLinkExistingFailureAuditCategoryNotLinked(t *testing.T) {
	h := newServiceHarness(t)
	_, err := h.service.LinkExisting(context.Background(), dto.AccountLinkInput{Provider: "hermes", ExternalID: "628123456789@s.whatsapp.net", Channel: "whatsapp"})
	if !errors.Is(err, domainidentity.ErrIdentityNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(h.audit.events) != 1 || h.audit.events[0].ErrorMessage != "not_linked" {
		t.Fatalf("audit = %#v", h.audit.events)
	}
}

func TestRenameRejectsFakeSpaceCharactersAsNewNames(t *testing.T) {
	for _, r := range []rune{0x034f, 0x115f, 0x1160, 0x17b4, 0x17b5, 0x180b, 0x180c, 0x180d, 0x180e, 0x180f, 0x2800, 0x3164, 0xffa0} {
		h := renameHarness(t)
		h.repository.otherNames = sharedNames()
		_, err := h.service.Rename(context.Background(), renameInput("Daddy"+string(r)+"Zeia"))
		var validation *serviceidentity.ValidationError
		if !errors.As(err, &validation) || validation.Field != "name" || h.repository.namesCalls != 0 {
			t.Fatalf("U+%04X: err = %v, names calls = %d, want the character rule", r, err, h.repository.namesCalls)
		}
		assertNoWrites(t, h)
	}
}

func TestRenameCollidesWithStoredNamesContainingInvisibleOrWideCharacters(t *testing.T) {
	spaced := []string{"Daddy \u200b Zeia", "Daddy\ufe0f Zeia", "Daddy Zeia\ufe0f", "Daddy  \u2060 Zeia", "\uff24\uff41\uff44\uff44\uff59 \uff3a\uff45\uff49\uff41"}
	glued := []string{
		"Daddy\ufe00Zeia", "Daddy\u034fZeia", "Daddy\u3164Zeia", "Daddy\u2800Zeia", "Daddy\u115fZeia", "Daddy\u1160Zeia", "Daddy\uffa0Zeia",
		"Daddy\u17b4Zeia", "Daddy\u17b5Zeia", "Daddy\u180bZeia", "Daddy\u180eZeia", "Daddy\u00adZeia", "Daddy\U000e0001Zeia", "Daddy\u200bZeia",
	}
	check := func(stored, candidate string) {
		h := renameHarness(t)
		h.repository.otherNames = []string{stored}
		_, err := h.service.Rename(context.Background(), renameInput(candidate))
		var validation *serviceidentity.ValidationError
		if !errors.As(err, &validation) || validation.Reason != collisionReason || h.repository.namesCalls != 1 {
			t.Fatalf("stored %+q vs %q: err = %v, names calls = %d", stored, candidate, err, h.repository.namesCalls)
		}
		assertNoWrites(t, h)
	}
	for _, stored := range spaced {
		check(stored, "Daddy Zeia")
	}
	for _, stored := range glued {
		check(stored, "DaddyZeia")
	}
}

func TestAccountNameKeyDropsInvisiblesBeforeCollapsingSpaces(t *testing.T) {
	if got, want := accountNameKey("Daddy \u200b Zeia"), accountNameKey("Daddy Zeia"); got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
}

func TestAccountNameKeyDropsEveryDefaultIgnorableCodePoint(t *testing.T) {
	// Unicode DerivedCoreProperties Default_Ignorable_Code_Point (Unicode 15/16), written out by hand
	// because the data file is not available offline; plus U+2800 by controller decision.
	ranges := [][2]rune{
		{0x00ad, 0x00ad}, {0x034f, 0x034f}, {0x061c, 0x061c}, {0x115f, 0x1160}, {0x17b4, 0x17b5}, {0x180b, 0x180f},
		{0x200b, 0x200f}, {0x202a, 0x202e}, {0x2060, 0x206f}, {0x2800, 0x2800}, {0x3164, 0x3164}, {0xfe00, 0xfe0f}, {0xfeff, 0xfeff},
		{0xffa0, 0xffa0}, {0xfff0, 0xfff8}, {0x1bca0, 0x1bca3}, {0x1d173, 0x1d17a}, {0xe0000, 0xe0fff},
	}
	for _, rg := range ranges {
		for r := rg[0]; r <= rg[1]; r++ {
			if got := accountNameKey("a" + string(r) + "b"); got != "ab" {
				t.Fatalf("U+%04X not dropped from the key: %q", r, got)
			}
		}
	}
}

func TestRenameAllowsVariationSelectorEmojiNames(t *testing.T) {
	h := renameHarness(t)
	got, err := h.service.Rename(context.Background(), renameInput("Rheina\u2764\ufe0f"))
	if err != nil || got.Status != StatusRenamed || h.repository.updatedName != "Rheina\u2764\ufe0f" {
		t.Fatalf("result = %+v, err = %v, stored %+q", got, err, h.repository.updatedName)
	}
}

func TestRenameFullWidthStoredNameCollidesWithPlainName(t *testing.T) {
	h := renameHarness(t)
	h.repository.otherNames = []string{"\uff24\uff41\uff44\uff44\uff59 \uff3a\uff45\uff49\uff41"}
	_, err := h.service.Rename(context.Background(), renameInput("Daddy Zeia"))
	var validation *serviceidentity.ValidationError
	if !errors.As(err, &validation) || validation.Reason != collisionReason || h.repository.namesCalls != 1 {
		t.Fatalf("err = %v, names calls = %d", err, h.repository.namesCalls)
	}
}

func TestNameReasonsArePinned(t *testing.T) {
	if ReasonNameTaken != "is already used by another member of your Spaces" ||
		ReasonNameLength != "must contain 3 to 100 characters" ||
		ReasonNameCharacters != "contains characters that are not allowed" {
		t.Fatalf("reasons changed: %q / %q / %q", ReasonNameTaken, ReasonNameLength, ReasonNameCharacters)
	}
}

func TestNormalizeAccountNameReportsLengthAndCharacterReasons(t *testing.T) {
	for raw, reason := range map[string]string{"ab": ReasonNameLength, strings.Repeat("a", 101): ReasonNameLength, "Jane\nDoe": ReasonNameCharacters, "Jane<Doe": ReasonNameCharacters} {
		_, err := normalizeAccountName(raw)
		var validation *serviceidentity.ValidationError
		if !errors.As(err, &validation) || validation.Field != "name" || validation.Reason != reason {
			t.Fatalf("%q: err = %v, want reason %q", raw, err, reason)
		}
	}
}

func TestNormalizeAccountNameAcceptsCountryFlag(t *testing.T) {
	got, err := normalizeAccountName("Budi \U0001F1EE\U0001F1E9")
	if err != nil || got != "Budi \U0001F1EE\U0001F1E9" {
		t.Fatalf("got %+q, err %v", got, err)
	}
}

func TestAccountNameKeyTreatsLatinLookAlikesAsTheSameName(t *testing.T) {
	groups := [][]string{
		{"Budi", "Bud\u0131", "Budi\u0307", "Bud\u0131\u0307", "BUDI", "Bud\u00ed", "Budi\u0301"},
		{"Raj", "Ra\u0237", "Raj\u0307", "Ra\u0237\u0307", "Ra\u0135"},
		{"Budi", "Budi \u0307"},
		{"Jose Zeia", "Jos\u00e9 Zeia", "Jose\u0301 Zeia", "JOS\u00c9 ZEIA"},
		{"Zoe Putri", "Zo\u00eb Putri", "Zoe\u0308 Putri"},
		{"Daddy Zeia", "\uff24\uff41\uff44\uff44\uff59 \uff3a\uff45\uff49\uff41", "Daddy\u00a0Zeia", "Daddy \u200b Zeia", "Daddy Zeia\ufe0f"},
	}
	for _, group := range groups {
		want := accountNameKey(group[0])
		for _, v := range group[1:] {
			if got := accountNameKey(v); got != want {
				t.Fatalf("%+q key = %+q, want %+q (same as %+q)", v, got, want, group[0])
			}
		}
	}
}

func TestAccountNameKeyKeepsMarksOnNonLatinBases(t *testing.T) {
	// Devanagari and Arabic marks (including Arabic harakat, an accepted limitation: harakat-only
	// variants of an Arabic name are NOT detected as the same name) must keep names distinct.
	pairs := [][2]string{
		{"\u0915\u093f", "\u0915"},                         // Devanagari ki vs ka
		{"\u0915\u0902", "\u0915"},                         // Devanagari anusvara (Mn)
		{"\u0643\u062a\u0628", "\u0643\u064e\u062a\u0628"}, // Arabic with fatha
		{"Budi", "Budi \u0915"},
	}
	for _, p := range pairs {
		if accountNameKey(p[0]) == accountNameKey(p[1]) {
			t.Fatalf("%+q and %+q share a key", p[0], p[1])
		}
	}
}

func TestRenameLatinLookAlikesCollideBothDirections(t *testing.T) {
	lookAlikes := []string{"Bud\u0131", "Budi\u0307", "Bud\u0131\u0307", "Bud\u00ed"}
	check := func(stored, candidate string) {
		h := renameHarness(t)
		h.repository.otherNames = []string{stored}
		_, err := h.service.Rename(context.Background(), renameInput(candidate))
		var validation *serviceidentity.ValidationError
		if !errors.As(err, &validation) || validation.Reason != ReasonNameTaken || h.repository.namesCalls != 1 {
			t.Fatalf("stored %+q vs %+q: err = %v, names calls = %d", stored, candidate, err, h.repository.namesCalls)
		}
		assertNoWrites(t, h)
	}
	for _, v := range lookAlikes {
		check("Budi", v)
		check(v, "Budi")
	}
	check("Jose", "Jos\u00e9")
	check("Jos\u00e9", "Jose")
	check("Zo\u00eb Putri", "Zoe Putri")
}

func TestRenameStoresTheNameAsTypedNotTheComparisonKey(t *testing.T) {
	h := renameHarness(t)
	h.repository.otherNames = []string{"Somebody Else"}
	got, err := h.service.Rename(context.Background(), renameInput("bud\u0131 jos\u00e9"))
	if err != nil || h.repository.updatedName != "Bud\u0131 Jos\u00e9" || got.Name != h.repository.updatedName {
		t.Fatalf("result = %+v, err = %v, stored %+q", got, err, h.repository.updatedName)
	}
}
