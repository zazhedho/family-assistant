package serviceaudit

import (
	"context"
	"errors"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/internal/dto"
	interfaceaudit "starter-kit/internal/interfaces/audit"
	"starter-kit/pkg/filter"
	"starter-kit/utils"
	"strings"
	"time"
)

type AuditService struct {
	AuditRepo interfaceaudit.RepoAuditInterface
}

func NewAuditService(auditRepo interfaceaudit.RepoAuditInterface) *AuditService {
	return &AuditService{
		AuditRepo: auditRepo,
	}
}

func (s *AuditService) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	if strings.TrimSpace(req.Action) == "" {
		return errors.New("action is required")
	}
	if strings.TrimSpace(req.Resource) == "" {
		return errors.New("resource is required")
	}
	if strings.TrimSpace(req.Status) == "" {
		return errors.New("status is required")
	}

	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}

	before := utils.RedactSensitivePayload(req.BeforeData)
	after := utils.RedactSensitivePayload(req.AfterData)
	meta := utils.RedactSensitivePayload(req.Metadata)

	data := domainaudit.AuditTrail{
		ID:           utils.CreateUUID(),
		OccurredAt:   occurredAt,
		ActorUserID:  utils.NormalizeUUIDPointer(req.ActorUserID),
		ActorRole:    strings.TrimSpace(req.ActorRole),
		Action:       utils.HumanizeKey(req.Action),
		Resource:     strings.TrimSpace(req.Resource),
		ResourceID:   strings.TrimSpace(req.ResourceID),
		Status:       utils.HumanizeKey(req.Status),
		Message:      strings.TrimSpace(req.Message),
		ErrorMessage: strings.TrimSpace(req.ErrorMessage),
		RequestID:    strings.TrimSpace(req.RequestID),
		IPAddress:    strings.TrimSpace(req.IPAddress),
		UserAgent:    strings.TrimSpace(req.UserAgent),
		BeforeData:   utils.JsonEncode(before),
		AfterData:    utils.JsonEncode(after),
		Metadata:     utils.JsonEncode(meta),
		CreatedAt:    time.Now(),
	}

	return s.AuditRepo.Store(ctx, data)
}

func (s *AuditService) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	items, total, err := s.AuditRepo.GetAll(ctx, params)
	if err != nil {
		return nil, 0, err
	}

	return toAuditResponses(items), total, nil
}

func (s *AuditService) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	item, err := s.AuditRepo.GetByID(ctx, id)
	if err != nil {
		return dto.AuditTrailResponse{}, err
	}

	return toAuditResponse(item), nil
}

var _ interfaceaudit.ServiceAuditInterface = (*AuditService)(nil)
