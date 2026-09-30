package handlerreminder

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"family-assistant/internal/authscope"
	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	"family-assistant/internal/dto"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"
	servicereminder "family-assistant/internal/services/reminder"
	"family-assistant/pkg/response"
	"family-assistant/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ReminderHandler struct {
	Service     interfacereminder.ServiceReminderInterface
	Resolver    interfaceidentity.UserResolver
	Permissions interfaceidentity.PermissionLoader
}

func NewReminderHandler(service interfacereminder.ServiceReminderInterface, resolver interfaceidentity.UserResolver, permissions interfaceidentity.PermissionLoader) *ReminderHandler {
	return &ReminderHandler{Service: service, Resolver: resolver, Permissions: permissions}
}

type createRequest struct {
	SpaceID          string `json:"space_id,omitempty"`
	Title            string `json:"title"`
	Description      string `json:"description,omitempty"`
	ScheduledAt      string `json:"scheduled_at"`
	AssigneeMemberID string `json:"assignee_member_id,omitempty"`
}

type reminderResponse struct {
	ID               string  `json:"id"`
	SpaceID          string  `json:"space_id"`
	Title            string  `json:"title"`
	Description      string  `json:"description,omitempty"`
	AssigneeMemberID *string `json:"assignee_member_id,omitempty"`
	Status           string  `json:"status"`
	ScheduledAt      string  `json:"scheduled_at"`
}

func (h *ReminderHandler) Create(ctx *gin.Context) {
	var request createRequest
	if err := decodeSingleJSON(ctx, &request); err != nil {
		h.writeError(ctx, &serviceauthorization.ValidationError{Field: "body", Reason: "is invalid"})
		return
	}
	spaceID, err := utils.ParseUUID(request.SpaceID, "space_id", false)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	scheduledAt, err := utils.ParseRFC3339(request.ScheduledAt, "scheduled_at", true)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	assigneeID, err := utils.ParseOptionalUUID(request.AssigneeMemberID, "assignee_member_id")
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	actor, err := h.actor(ctx, spaceID)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	if h.Service == nil {
		h.writeError(ctx, errors.New("reminder service is not configured"))
		return
	}
	reminder, err := h.Service.Create(ctx.Request.Context(), actor, dto.ReminderCreateInput{
		Title: request.Title, Description: request.Description, ScheduledAt: *scheduledAt,
		Space: actor.SpaceID, AssigneeMemberID: assigneeID,
	})
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, response.Response(http.StatusCreated, "Reminder created successfully", utils.GenerateLogId(ctx), toResponse(reminder)))
}

func (h *ReminderHandler) List(ctx *gin.Context) {
	spaceID, err := utils.ParseUUID(ctx.Query("space_id"), "space_id", false)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	actor, err := h.actor(ctx, spaceID)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	status, err := domainreminder.ParseStatus(ctx.Query("status"))
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	input := dto.ReminderListInput{
		Space: actor.SpaceID, Status: status,
	}
	if input.From, err = utils.ParseRFC3339(ctx.Query("from"), "from", false); err != nil {
		h.writeError(ctx, err)
		return
	}
	if input.To, err = utils.ParseRFC3339(ctx.Query("to"), "to", false); err != nil {
		h.writeError(ctx, err)
		return
	}
	if h.Service == nil {
		h.writeError(ctx, errors.New("reminder service is not configured"))
		return
	}
	reminders, err := h.Service.List(ctx.Request.Context(), actor, input)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	data := make([]reminderResponse, 0, len(reminders))
	for i := range reminders {
		data = append(data, toResponse(&reminders[i]))
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "Reminders retrieved successfully", utils.GenerateLogId(ctx), data))
}

func (h *ReminderHandler) Complete(ctx *gin.Context) {
	spaceID, err := utils.ParseUUID(ctx.Query("space_id"), "space_id", false)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	reminderID, err := utils.ParseUUID(ctx.Param("reminder_id"), "reminder_id", false)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	if reminderID == "" {
		h.writeError(ctx, &serviceauthorization.ValidationError{Field: "reminder_id", Reason: "is required"})
		return
	}
	actor, err := h.actor(ctx, spaceID)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	if h.Service == nil {
		h.writeError(ctx, errors.New("reminder service is not configured"))
		return
	}
	reminder, err := h.Service.Complete(ctx.Request.Context(), actor, actor.SpaceID, reminderID)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "Reminder completed successfully", utils.GenerateLogId(ctx), toResponse(reminder)))
}

func (h *ReminderHandler) actor(ctx *gin.Context, selector string) (domainidentity.ActorContext, error) {
	scope := authscope.FromContext(ctx.Request.Context())
	userID := strings.TrimSpace(scope.UserID)
	if userID == "" {
		return domainidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	if h.Resolver == nil {
		return domainidentity.ActorContext{}, errors.New("user resolver is not configured")
	}
	actor, err := h.Resolver.ResolveUser(ctx.Request.Context(), userID, "http")
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	actor, err = serviceidentity.SelectSpace(ctx.Request.Context(), actor, selector, h.Permissions)
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	if scope.IsImpersonated {
		actor.InitiatorUserID = strings.TrimSpace(scope.OriginalUserID)
		actor.InitiatorRoleName = strings.TrimSpace(scope.OriginalRole)
	}
	return actor, nil
}

func (h *ReminderHandler) writeError(ctx *gin.Context, err error) {
	writeReminderError(ctx, err)
}

func writeReminderError(ctx *gin.Context, err error) {
	status, message := reminderHTTPError(err)
	logID := utils.GenerateLogId(ctx)
	if status == http.StatusInternalServerError {
		ctx.JSON(status, response.InternalServerError(logID))
		return
	}
	ctx.JSON(status, response.ErrorResponse(status, http.StatusText(status), logID, message))
}

func reminderHTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, serviceidentity.ErrUnauthenticated):
		return http.StatusUnauthorized, "authentication required"
	case errors.Is(err, serviceauthorization.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, serviceauthorization.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, serviceauthorization.ErrInvalidResource), errors.Is(err, utils.ErrInvalidUUID),
		errors.Is(err, utils.ErrInvalidTime), errors.Is(err, domainreminder.ErrInvalidStatus):
		return http.StatusBadRequest, "invalid input"
	case errors.Is(err, servicereminder.ErrConflict):
		return http.StatusConflict, "conflict"
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}

func decodeSingleJSON(ctx *gin.Context, destination any) error {
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

func toResponse(reminder *domainreminder.Reminder) reminderResponse {
	if reminder == nil {
		return reminderResponse{}
	}
	return reminderResponse{ID: reminder.ID, SpaceID: reminder.SpaceID, Title: reminder.Title, Description: reminder.Description, AssigneeMemberID: reminder.AssigneeMemberID, Status: string(reminder.Status), ScheduledAt: reminder.ScheduledAt.Format(time.RFC3339Nano)}
}
