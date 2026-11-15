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
	if h.repository.takenUserID != "user-1" || h.repository.takenName != "Rheina Putri" {
		t.Fatalf("collision check = %q %q", h.repository.takenUserID, h.repository.takenName)
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
	if h.repository.takenCalls != 0 {
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

func TestRenameRejectsNameUsedInSharedSpaces(t *testing.T) {
	for _, name := range []string{"Daddy Zeia", "daddy zeia", "DADDY ZEIA", "  Daddy    Zeia  ", "Daddy\u200bZeia"} {
		h := renameHarness(t)
		h.repository.taken = true
		_, err := h.service.Rename(context.Background(), renameInput(name))
		var validation *serviceidentity.ValidationError
		if !errors.As(err, &validation) || validation.Field != "name" {
			t.Fatalf("%q: err = %v", name, err)
		}
		if strings.Contains(strings.ToLower(validation.Reason), "daddy") {
			t.Fatalf("reason echoes name: %q", validation.Reason)
		}
		assertNoWrites(t, h)
	}
}

func TestRenameAllowsNameUsedOnlyOutsideSharedSpaces(t *testing.T) {
	h := renameHarness(t)
	h.repository.taken = false
	got, err := h.service.Rename(context.Background(), renameInput("Daddy Zeia"))
	if err != nil || got.Status != StatusRenamed || h.repository.updateCalls != 1 {
		t.Fatalf("result = %+v, err = %v, updates = %d", got, err, h.repository.updateCalls)
	}
}

func TestRenameCollisionCheckErrorBlocksWrite(t *testing.T) {
	h := renameHarness(t)
	h.repository.takenErr = errors.New("db down")
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
		"collision": func(h *serviceHarness) { h.repository.taken = true },
		"update":    func(h *serviceHarness) { h.repository.updateErr = errors.New("write failed for " + attempted) },
		"taken":     func(h *serviceHarness) { h.repository.takenErr = errors.New("check failed for " + attempted) },
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

func TestRenameIgnoresRepositoryAbsence(t *testing.T) {
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
