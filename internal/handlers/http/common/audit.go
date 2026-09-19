package handlercommon

import (
	"fmt"

	"github.com/zazhedho/family-assistant/internal/authscope"
	domainaudit "github.com/zazhedho/family-assistant/internal/domain/audit"
	interfaceaudit "github.com/zazhedho/family-assistant/internal/interfaces/audit"
	"github.com/zazhedho/family-assistant/pkg/logger"
	"github.com/zazhedho/family-assistant/utils"

	"github.com/gin-gonic/gin"
)

type AuditWriter struct {
	Service interfaceaudit.ServiceAuditInterface
	Scope   string
}

func NewAuditWriter(service interfaceaudit.ServiceAuditInterface, scope string) AuditWriter {
	return AuditWriter{Service: service, Scope: scope}
}

func (w AuditWriter) WriteAudit(ctx *gin.Context, event domainaudit.AuditEvent) {
	if w.Service == nil {
		return
	}

	scopeData := authscope.FromContext(ctx.Request.Context())
	if event.ActorUserID == "" && scopeData.ActorUserID() != "" {
		event.ActorUserID = scopeData.ActorUserID()
	}
	if event.ActorRole == "" && scopeData.ActorRole() != "" {
		event.ActorRole = scopeData.ActorRole()
	}
	event.RequestID = utils.GetRequestID(ctx)
	event.IPAddress = ctx.ClientIP()
	event.UserAgent = ctx.GetHeader("User-Agent")
	event.Metadata = utils.MergeMetadata(event.Metadata, utils.GetImpersonationMetadata(ctx))

	if err := w.Service.Store(ctx.Request.Context(), event); err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelWarn, fmt.Sprintf("[%s][Audit]; failed to store audit trail: %v", w.Scope, err))
	}
}
