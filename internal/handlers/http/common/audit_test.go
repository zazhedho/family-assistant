package handlercommon

import (
	"context"
	"errors"
	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	"github.com/zazhedho/family-assistant/internal/dto"
	"github.com/zazhedho/family-assistant/pkg/filter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type auditServiceMock struct {
	stored domainaudit.AuditEvent
}

func (m *auditServiceMock) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	m.stored = req
	return nil
}

func (m *auditServiceMock) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, errors.New("not implemented")
}

func (m *auditServiceMock) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, errors.New("not implemented")
}

func TestWriteAuditPreservesExplicitActorWhenContextIsPublic(t *testing.T) {
	auditService := &auditServiceMock{}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/audit-test", nil)

	NewAuditWriter(auditService, "AuditServiceTest").WriteAudit(ctx, domainaudit.AuditEvent{
		ActorUserID: "00000000-0000-0000-0000-000000000001",
		ActorRole:   "viewer",
		Action:      domainaudit.ActionLogin,
		Resource:    "auth",
		Status:      domainaudit.StatusSuccess,
	})

	if auditService.stored.ActorUserID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected explicit actor user to be preserved, got %q", auditService.stored.ActorUserID)
	}
	if auditService.stored.ActorRole != "viewer" {
		t.Fatalf("expected explicit actor role to be preserved, got %q", auditService.stored.ActorRole)
	}
}
