package handlerinvitation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainuser "family-assistant/internal/domain/user"
	"family-assistant/internal/dto"
	handlercommon "family-assistant/internal/handlers/http/common"
	interfaceaudit "family-assistant/internal/interfaces/audit"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceinvitation "family-assistant/internal/services/invitation"
	"family-assistant/pkg/messages"
	"family-assistant/pkg/response"
	"family-assistant/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var errUnauthenticated = errors.New("authentication required")

type userRepository interface {
	GetByID(context.Context, string) (domainuser.Users, error)
}

type InvitationHandler struct {
	Service interfaceinvitation.ServiceInvitationInterface
	Users   userRepository
	handlercommon.AuditWriter
}

type createRequest struct {
	InvitedEmail string `json:"invited_email"`
	RoleName     string `json:"role_name"`
	Role         string `json:"role"`
}

type acceptRequest struct {
	Token string `json:"token"`
}

type createResponse struct {
	Invitation *domaininvitation.Invitation `json:"invitation"`
	Token      string                       `json:"token"`
}

// NewInvitationHandler accepts the service and optional user/audit dependencies.
// Variadic dependencies keep construction small for tests and route wiring.
func NewInvitationHandler(service interfaceinvitation.ServiceInvitationInterface, dependencies ...any) *InvitationHandler {
	h := &InvitationHandler{Service: service}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case userRepository:
			h.Users = value
		case interfaceaudit.ServiceAuditInterface:
			h.AuditWriter = handlercommon.NewAuditWriter(value, "InvitationHandler")
		}
	}
	return h
}

func (h *InvitationHandler) Create(ctx *gin.Context) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeInvitationError(ctx, err)
		return
	}
	spaceID := strings.TrimSpace(ctx.Param("space_id"))
	if _, err := uuid.Parse(spaceID); err != nil {
		validationErr := &serviceauthorization.ValidationError{Field: "space_id", Reason: "must be a valid UUID"}
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, spaceID, validationErr)
		writeInvitationError(ctx, validationErr)
		return
	}
	var request createRequest
	if err := decodeInvitationJSON(ctx, &request); err != nil {
		validationErr := &serviceauthorization.ValidationError{Field: "body", Reason: "is invalid"}
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, spaceID, validationErr)
		writeInvitationError(ctx, validationErr)
		return
	}
	roleName := strings.TrimSpace(request.RoleName)
	if roleName == "" {
		roleName = strings.TrimSpace(request.Role)
	}
	if request.RoleName != "" && request.Role != "" && !strings.EqualFold(request.RoleName, request.Role) {
		validationErr := &serviceauthorization.ValidationError{Field: "role_name", Reason: "must not conflict with role"}
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, spaceID, validationErr)
		writeInvitationError(ctx, validationErr)
		return
	}
	if h.Service == nil {
		err := errors.New("invitation service is not configured")
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, spaceID, err)
		writeInvitationError(ctx, err)
		return
	}
	invitation, rawToken, err := h.Service.Create(withAuditProvenance(ctx), userID, dto.InvitationCreateInput{
		SpaceID: spaceID, InvitedEmail: request.InvitedEmail, RoleName: roleName,
	})
	if err != nil {
		writeInvitationError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, response.Response(http.StatusCreated, "Space invitation created successfully", utils.GenerateLogId(ctx), createResponse{
		Invitation: invitation,
		Token:      rawToken,
	}))
}

func (h *InvitationHandler) Accept(ctx *gin.Context) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeInvitationError(ctx, err)
		return
	}
	var request acceptRequest
	if err := decodeInvitationJSON(ctx, &request); err != nil {
		validationErr := &serviceauthorization.ValidationError{Field: "body", Reason: "is invalid"}
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", validationErr)
		writeInvitationError(ctx, validationErr)
		return
	}
	if h.Users == nil {
		err := errors.New("user repository is not configured")
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeInvitationError(ctx, err)
		return
	}
	user, err := h.Users.GetByID(ctx.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = errUnauthenticated
		}
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeInvitationError(ctx, err)
		return
	}
	if strings.TrimSpace(user.Id) == "" || strings.TrimSpace(user.Id) != userID {
		err := errUnauthenticated
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeInvitationError(ctx, err)
		return
	}
	if h.Service == nil {
		err := errors.New("invitation service is not configured")
		h.writeEarlyFailure(ctx, domainaudit.ActionCreate, "", err)
		writeInvitationError(ctx, err)
		return
	}
	member, err := h.Service.Accept(withAuditProvenance(ctx), request.Token, user)
	if err != nil {
		writeInvitationError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "Space invitation accepted successfully", utils.GenerateLogId(ctx), member))
}

func withAuditProvenance(ctx *gin.Context) context.Context {
	return serviceinvitation.WithAuditProvenance(ctx.Request.Context(), serviceinvitation.AuditProvenance{
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

func decodeInvitationJSON(ctx *gin.Context, destination any) error {
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

func (h *InvitationHandler) writeEarlyFailure(ctx *gin.Context, action, resourceID string, err error) {
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:       action,
		Resource:     "invitation",
		ResourceID:   resourceID,
		Status:       domainaudit.StatusFailed,
		Source:       "http",
		ErrorMessage: serviceinvitation.FailureCategory(err),
	})
}

func writeInvitationError(ctx *gin.Context, err error) {
	status, publicMessage := invitationHTTPError(err)
	logID := utils.GenerateLogId(ctx)
	if status == http.StatusInternalServerError {
		ctx.JSON(status, response.InternalServerError(logID))
		return
	}
	ctx.JSON(status, response.ErrorResponse(status, http.StatusText(status), logID, publicMessage))
}

func invitationHTTPError(err error) (int, string) {
	var validationErr *serviceauthorization.ValidationError
	switch {
	case errors.Is(err, errUnauthenticated):
		return http.StatusUnauthorized, "authentication required"
	case errors.Is(err, serviceinvitation.ErrInvalidInvitation):
		return http.StatusBadRequest, "invalid invitation"
	case errors.Is(err, serviceinvitation.ErrMembershipConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, serviceinvitation.ErrForbidden), errors.Is(err, serviceauthorization.ErrForbidden):
		return http.StatusForbidden, messages.AccessDenied
	case errors.Is(err, serviceinvitation.ErrNotFound), errors.Is(err, serviceauthorization.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound, messages.NotFound
	case errors.As(err, &validationErr):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, messages.MsgSomethingWrong
	}
}
