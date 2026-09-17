package serviceaudit

import (
	"context"
	"errors"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/pkg/filter"
	"strings"
	"testing"
)

type auditRepoMock struct {
	stored domainaudit.AuditTrail
	item   domainaudit.AuditTrail
	items  []domainaudit.AuditTrail
	total  int64
	err    error
}

func (m *auditRepoMock) Store(ctx context.Context, data domainaudit.AuditTrail) error {
	m.stored = data
	return m.err
}

func (m *auditRepoMock) GetByID(ctx context.Context, id string) (domainaudit.AuditTrail, error) {
	if m.err != nil {
		return domainaudit.AuditTrail{}, m.err
	}
	return m.item, nil
}

func (m *auditRepoMock) GetAll(ctx context.Context, params filter.BaseParams) ([]domainaudit.AuditTrail, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	return append([]domainaudit.AuditTrail{}, m.items...), m.total, nil
}

func (m *auditRepoMock) Update(ctx context.Context, data domainaudit.AuditTrail) error { return nil }
func (m *auditRepoMock) Delete(ctx context.Context, id string) error                   { return nil }
func (m *auditRepoMock) SoftDelete(ctx context.Context, id, deletedBy string) error {
	return nil
}

func TestGetAllDelegatesToRepository(t *testing.T) {
	repo := &auditRepoMock{
		items: []domainaudit.AuditTrail{
			{
				ID:        "audit-1",
				Action:    "refresh token",
				Resource:  "auth_token",
				Status:    domainaudit.StatusSuccess,
				Message:   "Renewed login session",
				AfterData: `{"email":"user@example.com"}`,
			},
		},
		total: 1,
	}
	service := NewAuditService(repo)

	items, total, err := service.GetAll(context.Background(), filter.BaseParams{Page: 1, Limit: 10})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(items) != 1 || items[0].ID != "audit-1" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[0].ResourceLabel != "Auth Token" {
		t.Fatalf("expected readable resource label, got %q", items[0].ResourceLabel)
	}
	if items[0].Summary != "Success: Renewed login session" {
		t.Fatalf("expected readable summary, got %q", items[0].Summary)
	}
	after, ok := items[0].AfterData.(map[string]any)
	if !ok || after["email"] != "user@example.com" {
		t.Fatalf("expected decoded after data, got %#v", items[0].AfterData)
	}
}

func TestGetByIDDelegatesToRepository(t *testing.T) {
	repo := &auditRepoMock{
		item: domainaudit.AuditTrail{ID: "audit-1", Action: "login", Resource: "auth", Status: domainaudit.StatusSuccess},
	}
	service := NewAuditService(repo)

	item, err := service.GetByID(context.Background(), "audit-1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if item.ID != "audit-1" {
		t.Fatalf("expected audit-1, got %s", item.ID)
	}
	if item.ActionLabel != "Login" {
		t.Fatalf("expected action label Login, got %q", item.ActionLabel)
	}
}

func TestGetByIDReturnsRepositoryError(t *testing.T) {
	service := NewAuditService(&auditRepoMock{err: errors.New("not found")})

	_, err := service.GetByID(context.Background(), "missing")
	if err == nil || err.Error() != "not found" {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestStoreSanitizesSensitivePayloadAndHumanizesValues(t *testing.T) {
	repo := &auditRepoMock{}
	service := NewAuditService(repo)

	err := service.Store(context.Background(), domainaudit.AuditEvent{
		Action:   "refresh_token",
		Resource: "auth_token",
		Status:   "failed",
		AfterData: map[string]any{
			"email":        "user@example.com",
			"password":     "SecretPassword1!",
			"refreshToken": "sensitive-refresh-token",
			"nested": map[string]any{
				"otp_code": "123456",
			},
			"events": []any{
				map[string]any{"access_token": "sensitive-access-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if repo.stored.Action != "refresh token" {
		t.Fatalf("expected humanized action, got %q", repo.stored.Action)
	}
	if repo.stored.Resource != "auth_token" {
		t.Fatalf("expected raw resource to remain queryable, got %q", repo.stored.Resource)
	}
	if repo.stored.Status != "failed" {
		t.Fatalf("expected status failed, got %q", repo.stored.Status)
	}
	if strings.Contains(repo.stored.AfterData, "SecretPassword1!") ||
		strings.Contains(repo.stored.AfterData, "sensitive-refresh-token") ||
		strings.Contains(repo.stored.AfterData, "sensitive-access-token") ||
		strings.Contains(repo.stored.AfterData, "123456") {
		t.Fatalf("expected sensitive values to be redacted, got %s", repo.stored.AfterData)
	}
}
