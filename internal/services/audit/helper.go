package serviceaudit

import (
	"encoding/json"
	"fmt"
	domainaudit "starter-kit/internal/domain/audit"
	"starter-kit/internal/dto"
	"starter-kit/utils"
	"strings"
)

func toAuditResponses(items []domainaudit.AuditTrail) []dto.AuditTrailResponse {
	responses := make([]dto.AuditTrailResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, toAuditResponse(item))
	}
	return responses
}

func toAuditResponse(item domainaudit.AuditTrail) dto.AuditTrailResponse {
	actionLabel := utils.TitleHumanized(item.Action)
	resourceLabel := utils.TitleHumanized(item.Resource)
	statusLabel := utils.TitleHumanized(item.Status)

	return dto.AuditTrailResponse{
		ID:            item.ID,
		OccurredAt:    item.OccurredAt,
		Actor:         dto.AuditActor{UserID: item.ActorUserID, Role: utils.TitleHumanized(item.ActorRole)},
		Action:        item.Action,
		ActionLabel:   actionLabel,
		Resource:      item.Resource,
		ResourceLabel: resourceLabel,
		ResourceID:    item.ResourceID,
		Status:        item.Status,
		StatusLabel:   statusLabel,
		Summary:       buildAuditSummary(statusLabel, item.Message, resourceLabel),
		Message:       item.Message,
		ErrorMessage:  item.ErrorMessage,
		RequestID:     item.RequestID,
		IPAddress:     item.IPAddress,
		UserAgent:     item.UserAgent,
		BeforeData:    decodeAuditJSON(item.BeforeData),
		AfterData:     decodeAuditJSON(item.AfterData),
		Metadata:      decodeAuditJSON(item.Metadata),
		CreatedAt:     item.CreatedAt,
	}
}

func buildAuditSummary(statusLabel, message, resourceLabel string) string {
	message = strings.TrimSpace(message)
	if message != "" {
		return fmt.Sprintf("%s: %s", statusLabel, message)
	}

	resourceLabel = strings.TrimSpace(resourceLabel)
	if resourceLabel == "" {
		return statusLabel
	}
	return fmt.Sprintf("%s: %s", statusLabel, resourceLabel)
}

func decodeAuditJSON(value string) any {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return nil
	}

	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return value
	}
	return decoded
}
