package handleridentity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	handlercommon "family-assistant/internal/handlers/http/common"
	interfaceaudit "family-assistant/internal/interfaces/audit"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/messages"
	"family-assistant/pkg/response"
	"family-assistant/utils"

	"github.com/gin-gonic/gin"
)

var (
	errUnauthenticated     = errors.New("authentication required")
	errInvalidIdentityBody = errors.New("invalid identity request")
)

type IdentityHandler struct {
	Service serviceidentity.LinkService
	handlercommon.AuditWriter
}

type issueResponse struct {
	Code string `json:"code"`
}

type revokeRequest struct {
	ExternalID string `json:"external_id" binding:"required"`
}

func NewIdentityHandler(service serviceidentity.LinkService, audits ...interfaceaudit.ServiceAuditInterface) *IdentityHandler {
	h := &IdentityHandler{Service: service}
	if len(audits) > 0 && audits[0] != nil {
		h.AuditWriter = handlercommon.NewAuditWriter(audits[0], "IdentityHandler")
	}
	return h
}

func (h *IdentityHandler) Issue(ctx *gin.Context) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeIdentityError(ctx, err)
		return
	}
	if h.Service == nil {
		err := errors.New("identity service is not configured")
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeIdentityError(ctx, err)
		return
	}
	code, err := h.Service.Issue(withAuditProvenance(ctx), userID, domainidentity.ProviderHermes)
	if err != nil {
		writeIdentityError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, response.Response(http.StatusCreated, "Identity link code created successfully", utils.GenerateLogId(ctx), issueResponse{Code: code}))
}

func (h *IdentityHandler) IssueLinkCode(ctx *gin.Context) {
	h.Issue(ctx)
}

func (h *IdentityHandler) Revoke(ctx *gin.Context) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		h.writeEarlyFailure(ctx, domainaudit.ActionDelete, "", err)
		writeIdentityError(ctx, err)
		return
	}
	var request revokeRequest
	if err := decodeStrictJSON(ctx, &request); err != nil || strings.TrimSpace(request.ExternalID) == "" {
		h.writeEarlyFailure(ctx, domainaudit.ActionDelete, "", errInvalidIdentityBody)
		writeIdentityError(ctx, errInvalidIdentityBody)
		return
	}
	if h.Service == nil {
		err := errors.New("identity service is not configured")
		h.writeEarlyFailure(ctx, domainaudit.ActionDelete, "", err)
		writeIdentityError(ctx, err)
		return
	}
	if err := h.Service.Revoke(withAuditProvenance(ctx), userID, domainidentity.ProviderHermes, request.ExternalID); err != nil {
		writeIdentityError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "External identity revoked successfully", utils.GenerateLogId(ctx), nil))
}

func (h *IdentityHandler) RevokeLink(ctx *gin.Context) {
	h.Revoke(ctx)
}

func withAuditProvenance(ctx *gin.Context) context.Context {
	return serviceidentity.WithAuditProvenance(ctx.Request.Context(), serviceidentity.AuditProvenance{
		Source:    "http",
		RequestID: utils.GetRequestID(ctx),
		IPAddress: ctx.ClientIP(),
		UserAgent: ctx.GetHeader("User-Agent"),
		Metadata:  utils.GetImpersonationMetadata(ctx),
	})
}

func authenticatedUserID(ctx *gin.Context) (string, error) {
	userID := strings.TrimSpace(authscope.FromContext(ctx.Request.Context()).UserID)
	if userID == "" {
		return "", errUnauthenticated
	}
	return userID, nil
}

func decodeStrictJSON(ctx *gin.Context, destination any) error {
	decoder := json.NewDecoder(ctx.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func (h *IdentityHandler) writeEarlyFailure(ctx *gin.Context, action, resourceID string, err error) {
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:       action,
		Resource:     "external_identity",
		ResourceID:   resourceID,
		Status:       domainaudit.StatusFailed,
		Source:       "http",
		Metadata:     map[string]any{"provider": domainidentity.ProviderHermes},
		ErrorMessage: serviceidentity.FailureCategory(err),
	})
}

func writeIdentityError(ctx *gin.Context, err error) {
	status, publicMessage := identityHTTPError(err)
	logID := utils.GenerateLogId(ctx)
	if status == http.StatusInternalServerError {
		ctx.JSON(status, response.InternalServerError(logID))
		return
	}
	ctx.JSON(status, response.ErrorResponse(status, http.StatusText(status), logID, publicMessage))
}

func identityHTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, errUnauthenticated):
		return http.StatusUnauthorized, "authentication required"
	case errors.Is(err, errInvalidIdentityBody):
		return http.StatusBadRequest, "invalid identity request"
	case errors.Is(err, domainidentity.ErrInvalidLinkToken), errors.Is(err, serviceidentity.ErrInvalidIdentityProvider), errors.Is(err, serviceidentity.ErrInvalidExternalIdentity):
		return http.StatusBadRequest, "invalid identity request"
	case errors.Is(err, domainidentity.ErrIdentityConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, domainidentity.ErrIdentityNotFound):
		return http.StatusNotFound, messages.NotFound
	default:
		return http.StatusInternalServerError, messages.MsgSomethingWrong
	}
}
