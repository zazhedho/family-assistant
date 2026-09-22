package serviceonboarding

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"
	domainrole "family-assistant/internal/domain/role"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceonboarding "family-assistant/internal/interfaces/onboarding"
	serviceidentity "family-assistant/internal/services/identity"

	"github.com/google/uuid"
)

type findResult struct {
	account domainonboarding.AccountRef
	err     error
}

type repositoryStub struct {
	found       domainonboarding.AccountRef
	findErr     error
	findResults []findResult
	findCalls   int
	createErr   error
	createCalls int
	created     domainonboarding.Registration
}

func (r *repositoryStub) FindByExternalIdentity(_ context.Context, _, _ string) (domainonboarding.AccountRef, error) {
	r.findCalls++
	if len(r.findResults) > 0 {
		result := r.findResults[0]
		r.findResults = r.findResults[1:]
		return result.account, result.err
	}
	if r.findErr != nil {
		return domainonboarding.AccountRef{}, r.findErr
	}
	if r.found != (domainonboarding.AccountRef{}) {
		return r.found, nil
	}
	return domainonboarding.AccountRef{}, domainidentity.ErrIdentityNotFound
}

func (r *repositoryStub) Create(_ context.Context, registration domainonboarding.Registration) error {
	r.createCalls++
	r.created = registration
	return r.createErr
}

type roleFinderStub struct {
	roles map[string]domainrole.Role
	errs  map[string]error
}

func (r *roleFinderStub) GetByName(_ context.Context, name string) (domainrole.Role, error) {
	if err := r.errs[name]; err != nil {
		return domainrole.Role{}, err
	}
	return r.roles[name], nil
}

type auditStoreStub struct {
	events []domainaudit.AuditEvent
}

func (s *auditStoreStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

type serviceHarness struct {
	service    *Service
	repository *repositoryStub
	roles      *roleFinderStub
	audit      *auditStoreStub
}

func newServiceHarness(t *testing.T) *serviceHarness {
	t.Helper()
	t.Setenv("MIN_INDEPENDENT_ACCOUNT_AGE", "18")
	repository := &repositoryStub{}
	roles := &roleFinderStub{
		roles: map[string]domainrole.Role{
			"viewer":      {Id: "viewer-role", Name: "viewer"},
			"space_owner": {Id: "space-owner-role", Name: "space_owner"},
		},
		errs: map[string]error{},
	}
	audit := &auditStoreStub{}
	service := NewService(repository, roles, audit)
	service.now = func() time.Time {
		return time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	}
	return &serviceHarness{service: service, repository: repository, roles: roles, audit: audit}
}

func validInput() dto.AccountRegistrationInput {
	return dto.AccountRegistrationInput{
		Name:       "Jane Doe",
		BirthDate:  "1990-05-20",
		Consent:    true,
		Provider:   "hermes",
		ExternalID: "profile-1",
		Channel:    "whatsapp",
	}
}

func TestRegisterCreatesExternalOnlyAccountAndPersonalSpace(t *testing.T) {
	h := newServiceHarness(t)
	got, err := h.service.Register(context.Background(), dto.AccountRegistrationInput{
		Name: " <b>jane doe</b> ", BirthDate: "1990-05-20", Consent: true,
		Provider: " HERMES ", ExternalID: " profile-1 ", Channel: "whatsapp",
	})
	if err != nil || got.Status != StatusCreated || uuid.Validate(got.UserID) != nil || uuid.Validate(got.SpaceID) != nil {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
	saved := h.repository.created
	if saved.User.Name != "Jane Doe" || saved.User.Email != "" || saved.User.Phone != "" || saved.User.Password != "" || saved.User.Role != "viewer" || saved.User.LoginProvider != "hermes" || saved.User.AgeVerificationMethod != "self_declared" {
		t.Fatalf("user = %+v", saved.User)
	}
	if saved.User.RoleId == nil || *saved.User.RoleId != "viewer-role" || saved.User.BirthDate == nil || saved.User.BirthDate.Format("2006-01-02") != "1990-05-20" {
		t.Fatalf("user role/birth date = %+v", saved.User)
	}
	if saved.Space.ID != got.SpaceID || saved.Space.CreatedByUserID != got.UserID || saved.Space.Name != "Jane Doe's Space" || saved.Space.Type != domainspace.TypePersonal || saved.Space.Category != domainspace.CategoryPersonal || saved.Space.Status != domainspace.StatusActive {
		t.Fatalf("space = %+v", saved.Space)
	}
	if saved.Member.UserID != got.UserID || saved.Member.SpaceID != got.SpaceID || saved.Member.RoleID != "space-owner-role" || saved.Member.Status != domainspace.StatusActive || uuid.Validate(saved.Member.ID) != nil {
		t.Fatalf("member = %+v", saved.Member)
	}
	if saved.Identity.UserID != got.UserID || saved.Identity.Provider != "hermes" || saved.Identity.ExternalID != "profile-1" || saved.Identity.Status != domainidentity.StatusActive || uuid.Validate(saved.Identity.ID) != nil {
		t.Fatalf("identity = %+v", saved.Identity)
	}
}

func TestRegisterCopiesWhatsAppPhoneToUser(t *testing.T) {
	h := newServiceHarness(t)
	input := validInput()
	input.ExternalID = "628123456789@s.whatsapp.net"

	if _, err := h.service.Register(context.Background(), input); err != nil {
		t.Fatalf("register: %v", err)
	}
	if got := h.repository.created.User.Phone; got != "628123456789" {
		t.Fatalf("phone = %q, want normalized WhatsApp phone", got)
	}
}

func TestRegisterDoesNotCopyNonWhatsAppIdentityToUserPhone(t *testing.T) {
	h := newServiceHarness(t)
	input := validInput()
	input.Channel = "telegram"
	input.ExternalID = "628123456789"

	if _, err := h.service.Register(context.Background(), input); err != nil {
		t.Fatalf("register: %v", err)
	}
	if got := h.repository.created.User.Phone; got != "" {
		t.Fatalf("phone = %q, want empty for non-WhatsApp identity", got)
	}
}

func TestRegisterReturnsExistingWithoutCreating(t *testing.T) {
	h := newServiceHarness(t)
	h.repository.found = domainonboarding.AccountRef{UserID: "user-1", SpaceID: "space-1"}
	h.repository.findErr = nil
	got, err := h.service.Register(context.Background(), validInput())
	if err != nil || got != (dto.AccountRegistrationResult{Status: StatusExisting, UserID: "user-1", SpaceID: "space-1"}) || h.repository.createCalls != 0 {
		t.Fatalf("result = %+v, create calls = %d, err = %v", got, h.repository.createCalls, err)
	}
}

func TestRegisterRecoversConcurrentIdentityConflict(t *testing.T) {
	h := newServiceHarness(t)
	h.repository.findResults = []findResult{
		{err: domainidentity.ErrIdentityNotFound},
		{account: domainonboarding.AccountRef{UserID: "winner-user", SpaceID: "winner-space"}},
	}
	h.repository.createErr = domainidentity.ErrIdentityConflict
	got, err := h.service.Register(context.Background(), validInput())
	if err != nil || got != (dto.AccountRegistrationResult{Status: StatusExisting, UserID: "winner-user", SpaceID: "winner-space"}) || h.repository.createCalls != 1 {
		t.Fatalf("result = %+v, create calls = %d, err = %v", got, h.repository.createCalls, err)
	}
}

func TestRegisterRejectsInvalidConsentNameAndBirthDateWithoutPersistence(t *testing.T) {
	tests := []struct {
		name, displayName, birthDate string
		consent                      bool
	}{
		{name: "no consent", displayName: "Jane Doe", birthDate: "1990-05-20", consent: false},
		{name: "whitespace", displayName: "   ", birthDate: "1990-05-20", consent: true},
		{name: "html only", displayName: "<script>x</script>", birthDate: "1990-05-20", consent: true},
		{name: "two characters", displayName: "Jo", birthDate: "1990-05-20", consent: true},
		{name: "101 characters", displayName: strings.Repeat("a", 101), birthDate: "1990-05-20", consent: true},
		{name: "empty date", displayName: "Jane Doe", birthDate: "", consent: true},
		{name: "malformed date", displayName: "Jane Doe", birthDate: "20-05-1990", consent: true},
		{name: "future date", displayName: "Jane Doe", birthDate: "2099-01-01", consent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newServiceHarness(t)
			_, err := h.service.Register(context.Background(), dto.AccountRegistrationInput{
				Name: tt.displayName, BirthDate: tt.birthDate, Consent: tt.consent,
				Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp",
			})
			var validation *serviceidentity.ValidationError
			if !errors.As(err, &validation) || h.repository.createCalls != 0 {
				t.Fatalf("error = %T %v, create calls = %d", err, err, h.repository.createCalls)
			}
		})
	}
}

func TestRegisterEnforcesAgeBoundaries(t *testing.T) {
	tests := []struct {
		name, birthDate string
		valid           bool
	}{
		{name: "birthday", birthDate: "2008-09-21", valid: true},
		{name: "day before birthday", birthDate: "2008-09-22", valid: false},
		{name: "leap day", birthDate: "2008-02-29", valid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newServiceHarness(t)
			input := validInput()
			input.BirthDate = tt.birthDate
			got, err := h.service.Register(context.Background(), input)
			if tt.valid {
				if err != nil || got.Status != StatusCreated {
					t.Fatalf("result = %+v, err = %v", got, err)
				}
				return
			}
			var validation *serviceidentity.ValidationError
			if !errors.As(err, &validation) || h.repository.createCalls != 0 {
				t.Fatalf("error = %T %v, create calls = %d", err, err, h.repository.createCalls)
			}
		})
	}
}

func TestRegisterRequiresExternalIdentity(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		external string
	}{
		{name: "missing provider", provider: "", external: "profile-1"},
		{name: "missing external id", provider: "hermes", external: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newServiceHarness(t)
			input := validInput()
			input.Provider = tt.provider
			input.ExternalID = tt.external
			got, err := h.service.Register(context.Background(), input)
			if !errors.Is(err, serviceidentity.ErrUnauthenticated) || got != (dto.AccountRegistrationResult{}) || h.repository.findCalls != 0 || h.repository.createCalls != 0 {
				t.Fatalf("result = %+v, err = %v, find calls = %d, create calls = %d", got, err, h.repository.findCalls, h.repository.createCalls)
			}
		})
	}
}

func TestRegisterMissingRoleFailsWithoutPersistence(t *testing.T) {
	tests := []struct {
		name string
		role string
	}{
		{name: "viewer", role: "viewer"},
		{name: "space owner", role: "space_owner"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newServiceHarness(t)
			h.roles.roles[tt.role] = domainrole.Role{}
			got, err := h.service.Register(context.Background(), validInput())
			if err == nil || got != (dto.AccountRegistrationResult{}) || h.repository.createCalls != 0 || len(h.audit.events) != 1 || h.audit.events[0].Status != domainaudit.StatusFailed {
				t.Fatalf("result = %+v, err = %v, create calls = %d, audits = %#v", got, err, h.repository.createCalls, h.audit.events)
			}
		})
	}
}

func TestRegisterUnexpectedRepositoryFailuresReturnNoIDs(t *testing.T) {
	t.Run("find", func(t *testing.T) {
		h := newServiceHarness(t)
		h.repository.findErr = errors.New("find failed")
		got, err := h.service.Register(context.Background(), validInput())
		if err == nil || got != (dto.AccountRegistrationResult{}) || h.repository.createCalls != 0 {
			t.Fatalf("result = %+v, err = %v, create calls = %d", got, err, h.repository.createCalls)
		}
	})
	t.Run("create", func(t *testing.T) {
		h := newServiceHarness(t)
		h.repository.createErr = errors.New("create failed")
		got, err := h.service.Register(context.Background(), validInput())
		if err == nil || got != (dto.AccountRegistrationResult{}) || h.repository.createCalls != 1 {
			t.Fatalf("result = %+v, err = %v, create calls = %d", got, err, h.repository.createCalls)
		}
	})
}

func TestRegisterAuditIncludesProviderAndChannelWithoutPrivateIdentityData(t *testing.T) {
	h := newServiceHarness(t)
	input := validInput()
	input.Provider = " HERMES "
	input.ExternalID = "profile-private-1"
	input.Channel = " whatsapp "
	if _, err := h.service.Register(context.Background(), input); err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(h.audit.events) != 1 {
		t.Fatalf("audit events = %#v", h.audit.events)
	}
	event := h.audit.events[0]
	if event.Resource != "external_account" || event.Action != domainaudit.ActionCreate || event.Status != domainaudit.StatusSuccess || event.ActorUserID == "" || event.Channel != "whatsapp" {
		t.Fatalf("audit event = %#v", event)
	}
	if len(event.Metadata) != 2 || event.Metadata["provider"] != "hermes" || event.Metadata["channel"] != "whatsapp" {
		t.Fatalf("audit metadata = %#v", event.Metadata)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal audit: %v", err)
	}
	for _, privateValue := range []string{input.BirthDate, input.ExternalID} {
		if strings.Contains(string(encoded), privateValue) {
			t.Fatalf("private value leaked into audit: %q in %s", privateValue, encoded)
		}
	}
}

func TestRegisterAcceptsValidUnicodeName(t *testing.T) {
	h := newServiceHarness(t)
	input := validInput()
	input.Name = " josé álvarez "
	got, err := h.service.Register(context.Background(), input)
	if err != nil || got.Status != StatusCreated || h.repository.created.User.Name != "José Álvarez" {
		t.Fatalf("result = %+v, user = %+v, err = %v", got, h.repository.created.User, err)
	}
}

func TestServiceSatisfiesRegistrar(t *testing.T) {
	var _ interfaceonboarding.ServiceOnboardingInterface = (*Service)(nil)
	var _ interfaceonboarding.RoleFinder = (*roleFinderStub)(nil)
	var _ interfaceonboarding.AuditStore = (*auditStoreStub)(nil)
}
