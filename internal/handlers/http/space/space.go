package handlerspace

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	handlercommon "family-assistant/internal/handlers/http/common"
	interfaceaudit "family-assistant/internal/interfaces/audit"
	interfacespace "family-assistant/internal/interfaces/space"
	serviceauthorization "family-assistant/internal/services/authorization"
	servicespace "family-assistant/internal/services/space"
	"family-assistant/pkg/messages"
	"family-assistant/pkg/response"
	"family-assistant/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var errUnauthenticated = errors.New("authentication required")

type SpaceHandler struct {
	Service interfacespace.ServiceSpaceInterface
	handlercommon.AuditWriter
}

type createRequest struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

func NewSpaceHandler(service interfacespace.ServiceSpaceInterface, auditService interfaceaudit.ServiceAuditInterface) *SpaceHandler {
	return &SpaceHandler{
		Service:     service,
		AuditWriter: handlercommon.NewAuditWriter(auditService, "SpaceHandler"),
	}
}

func (h *SpaceHandler) List(ctx *gin.Context) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		h.writeEarlyFailure(ctx, "list", "", err)
		writeSpaceError(ctx, err)
		return
	}
	if h.Service == nil {
		err := errors.New("space service is not configured")
		h.writeEarlyFailure(ctx, "list", "", err)
		writeSpaceError(ctx, err)
		return
	}
	spaces, err := h.Service.List(withAuditProvenance(ctx), userID)
	if err != nil {
		writeSpaceError(ctx, err)
		return
	}
	if spaces == nil {
		spaces = []domainspace.ResolvedMembership{}
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "Spaces retrieved successfully", utils.GenerateLogId(ctx), spaces))
}

func (h *SpaceHandler) Create(ctx *gin.Context) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		h.writeEarlyFailure(ctx, "create", "", err)
		writeSpaceError(ctx, err)
		return
	}
	var request createRequest
	if err := decodeSpaceJSON(ctx, &request); err != nil {
		validationErr := &serviceauthorization.ValidationError{Field: "body", Reason: "is invalid"}
		h.writeEarlyFailure(ctx, "create", "", validationErr)
		writeSpaceError(ctx, validationErr)
		return
	}
	if h.Service == nil {
		err := errors.New("space service is not configured")
		h.writeEarlyFailure(ctx, "create", "", err)
		writeSpaceError(ctx, err)
		return
	}
	space, err := h.Service.Create(withAuditProvenance(ctx), userID, dto.SpaceCreateInput{Name: request.Name, Category: request.Category})
	if err != nil {
		writeSpaceError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, response.Response(http.StatusCreated, "Shared space created successfully", utils.GenerateLogId(ctx), space))
}

func (h *SpaceHandler) Members(ctx *gin.Context) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		h.writeEarlyFailure(ctx, "list", "", err)
		writeSpaceError(ctx, err)
		return
	}
	spaceID := strings.TrimSpace(ctx.Param("space_id"))
	if _, err := uuid.Parse(spaceID); err != nil {
		validationErr := &serviceauthorization.ValidationError{Field: "space_id", Reason: "must be a valid UUID"}
		h.writeEarlyFailure(ctx, "list", "", validationErr)
		writeSpaceError(ctx, validationErr)
		return
	}
	if h.Service == nil {
		err := errors.New("space service is not configured")
		h.writeEarlyFailure(ctx, "list", spaceID, err)
		writeSpaceError(ctx, err)
		return
	}
	members, err := h.Service.Members(withAuditProvenance(ctx), userID, spaceID)
	if err != nil {
		writeSpaceError(ctx, err)
		return
	}
	if members == nil {
		members = []domainspace.ResolvedMembership{}
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "Space members retrieved successfully", utils.GenerateLogId(ctx), members))
}

func withAuditProvenance(ctx *gin.Context) context.Context {
	return servicespace.WithAuditProvenance(ctx.Request.Context(), servicespace.AuditProvenance{
		RequestID: utils.GetRequestID(ctx),
		IPAddress: ctx.ClientIP(),
		UserAgent: ctx.GetHeader("User-Agent"),
		Metadata:  utils.GetImpersonationMetadata(ctx),
	})
}

func (h *SpaceHandler) writeEarlyFailure(ctx *gin.Context, action, resourceID string, err error) {
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:       action,
		Resource:     "space",
		ResourceID:   resourceID,
		Status:       domainaudit.StatusFailed,
		Source:       "http",
		ErrorMessage: servicespace.FailureCategory(err),
	})
}

func authenticatedUserID(ctx *gin.Context) (string, error) {
	userID := strings.TrimSpace(authscope.FromContext(ctx.Request.Context()).UserID)
	if userID == "" {
		return "", errUnauthenticated
	}
	return userID, nil
}

func decodeSpaceJSON(ctx *gin.Context, destination any) error {
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

func writeSpaceError(ctx *gin.Context, err error) {
	status, publicMessage := spaceHTTPError(err)
	logID := utils.GenerateLogId(ctx)
	if status == http.StatusInternalServerError {
		ctx.JSON(status, response.InternalServerError(logID))
		return
	}
	ctx.JSON(status, response.ErrorResponse(status, http.StatusText(status), logID, publicMessage))
}

func spaceHTTPError(err error) (int, string) {
	var validationErr *serviceauthorization.ValidationError
	switch {
	case errors.Is(err, errUnauthenticated):
		return http.StatusUnauthorized, "authentication required"
	case errors.Is(err, servicespace.ErrForbidden), errors.Is(err, serviceauthorization.ErrForbidden):
		return http.StatusForbidden, messages.AccessDenied
	case errors.Is(err, servicespace.ErrNotFound), errors.Is(err, serviceauthorization.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound, messages.NotFound
	case errors.As(err, &validationErr):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, messages.MsgSomethingWrong
	}
}
