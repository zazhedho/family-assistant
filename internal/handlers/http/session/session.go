package handlersession

import (
	"fmt"
	"net/http"
	"starter-kit/internal/authscope"
	domainaudit "starter-kit/internal/domain/audit"
	handlercommon "starter-kit/internal/handlers/http/common"
	interfaceaudit "starter-kit/internal/interfaces/audit"
	interfacesession "starter-kit/internal/interfaces/session"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/utils"

	"github.com/gin-gonic/gin"
)

type HandlerSession struct {
	Service interfacesession.ServiceSessionInterface
	handlercommon.AuditWriter
}

func NewSessionHandler(s interfacesession.ServiceSessionInterface, auditService interfaceaudit.ServiceAuditInterface) *HandlerSession {
	return &HandlerSession{
		Service:     s,
		AuditWriter: handlercommon.NewAuditWriter(auditService, "SessionHandler"),
	}
}

func (h *HandlerSession) GetActiveSessions(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[SessionHandler][GetActiveSessions]"
	reqCtx := ctx.Request.Context()

	scope := authscope.FromContext(reqCtx)
	if scope.UserID == "" {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; userId not found in context", logPrefix))
		res := response.Unauthorized(logId, "User is not authenticated. Please login again.")
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	token, _ := ctx.Get("token")
	currentSession, err := h.Service.GetSessionByToken(reqCtx, token.(string))
	currentSessionID := ""
	if err == nil && currentSession != nil {
		currentSessionID = currentSession.SessionID
	}

	sessions, err := h.Service.GetUserSessions(reqCtx, scope.UserID, currentSessionID)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetUserSessions; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "success", logId, map[string]any{
		"sessions": sessions,
		"total":    len(sessions),
	})
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerSession) RevokeSession(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[SessionHandler][RevokeSession]"
	reqCtx := ctx.Request.Context()

	sessionID := ctx.Param("session_id")
	if sessionID == "" {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "session",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to revoke a login session",
			ErrorMessage: "Session ID is required",
		})
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = "session_id is required"
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	scope := authscope.FromContext(reqCtx)
	if scope.UserID == "" {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; userId not found in context", logPrefix))
		res := response.Unauthorized(logId, "User is not authenticated. Please login again.")
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	session, err := h.Service.GetSessionBySessionID(reqCtx, sessionID)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "session",
			ResourceID:   sessionID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to revoke a login session",
			ErrorMessage: "The requested session was not found",
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetSessionBySessionID; Error: %+v", logPrefix, err))
		res := response.Response(http.StatusNotFound, messages.MsgSomethingWrong, logId, nil)
		res.Error = "session not found"
		ctx.JSON(http.StatusNotFound, res)
		return
	}

	if session.UserID != scope.UserID {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "session",
			ResourceID:   sessionID,
			Status:       domainaudit.StatusFailed,
			Message:      "Blocked unauthorized session revocation",
			ErrorMessage: "The session belongs to another user",
			AfterData: map[string]any{
				"session_user_id": session.UserID,
			},
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; unauthorized session revocation attempt", logPrefix))
		res := response.Forbidden(logId, messages.AccessDenied)
		ctx.JSON(http.StatusForbidden, res)
		return
	}

	if err := h.Service.DestroySession(reqCtx, sessionID); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "session",
			ResourceID:   sessionID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to revoke a login session",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.DestroySession; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionDelete,
		Resource:   "session",
		ResourceID: sessionID,
		Status:     domainaudit.StatusSuccess,
		Message:    "Revoked a login session",
	})

	res := response.Response(http.StatusOK, "Session revoked successfully", logId, nil)
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerSession) RevokeAllOtherSessions(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[SessionHandler][RevokeAllOtherSessions]"
	reqCtx := ctx.Request.Context()

	scope := authscope.FromContext(reqCtx)
	if scope.UserID == "" {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; userId not found in context", logPrefix))
		res := response.Unauthorized(logId, "User is not authenticated. Please login again.")
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	token, _ := ctx.Get("token")
	currentSession, err := h.Service.GetSessionByToken(reqCtx, token.(string))
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "session",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to revoke other login sessions",
			ErrorMessage: "Could not identify the current session",
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetSessionByToken; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	if err := h.Service.DestroyOtherSessions(reqCtx, scope.UserID, currentSession.SessionID); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "session",
			ResourceID:   currentSession.SessionID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to revoke other login sessions",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.DestroyOtherSessions; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionDelete,
		Resource:   "session",
		ResourceID: currentSession.SessionID,
		Status:     domainaudit.StatusSuccess,
		Message:    "Revoked all other login sessions",
	})

	res := response.Response(http.StatusOK, "All other sessions revoked successfully", logId, nil)
	ctx.JSON(http.StatusOK, res)
}
