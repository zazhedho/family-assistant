package handlerreminder

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"family-assistant/internal/authscope"
	domainreminder "family-assistant/internal/domain/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"
	servicereminder "family-assistant/internal/services/reminder"
	"family-assistant/pkg/response"
	"family-assistant/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ReminderHandler struct {
	Service     servicereminder.Service
	Resolver    serviceidentity.UserResolver
	Permissions serviceidentity.PermissionLoader
}

func NewReminderHandler(service servicereminder.Service, resolver serviceidentity.UserResolver, permissions serviceidentity.PermissionLoader) *ReminderHandler {
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
	spaceID, err := parseUUID(request.SpaceID, "space_id")
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	scheduledAt, err := parseHTTPTime(request.ScheduledAt, "scheduled_at", true)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	assigneeID, err := parseOptionalUUID(request.AssigneeMemberID, "assignee_member_id")
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
	reminder, err := h.Service.Create(ctx.Request.Context(), actor, servicereminder.CreateInput{
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
	for _, field := range []string{"scope", "target_member_id"} {
		if _, present := ctx.GetQuery(field); present {
			h.writeError(ctx, &serviceauthorization.ValidationError{Field: field, Reason: "is not supported"})
			return
		}
	}
	spaceID, err := parseUUID(ctx.Query("space_id"), "space_id")
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	actor, err := h.actor(ctx, spaceID)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	status, err := statusFilter(ctx.Query("status"))
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	input := servicereminder.ListInput{
		Space: actor.SpaceID, Status: status,
	}
	if input.From, err = parseHTTPTime(ctx.Query("from"), "from", false); err != nil {
		h.writeError(ctx, err)
		return
	}
	if input.To, err = parseHTTPTime(ctx.Query("to"), "to", false); err != nil {
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
	spaceID, err := parseUUID(ctx.Query("space_id"), "space_id")
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
	reminder, err := h.Service.Complete(ctx.Request.Context(), actor, actor.SpaceID, ctx.Param("reminder_id"))
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "Reminder completed successfully", utils.GenerateLogId(ctx), toResponse(reminder)))
}

func (h *ReminderHandler) actor(ctx *gin.Context, selector string) (serviceidentity.ActorContext, error) {
	scope := authscope.FromContext(ctx.Request.Context())
	userID := strings.TrimSpace(scope.UserID)
	if userID == "" {
		return serviceidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	if h.Resolver == nil {
		return serviceidentity.ActorContext{}, errors.New("user resolver is not configured")
	}
	actor, err := h.Resolver.ResolveUser(ctx.Request.Context(), userID, "http")
	if err != nil {
		return serviceidentity.ActorContext{}, err
	}
	actor, err = serviceidentity.SelectSpace(ctx.Request.Context(), actor, selector, h.Permissions)
	if err != nil {
		return serviceidentity.ActorContext{}, err
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
	case errors.Is(err, serviceauthorization.ErrInvalidResource):
		return http.StatusBadRequest, "invalid input"
	case errors.Is(err, servicereminder.ErrConflict):
		return http.StatusConflict, "conflict"
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}

func parseHTTPTime(value, field string, required bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return nil, &serviceauthorization.ValidationError{Field: field, Reason: "is required"}
		}
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, &serviceauthorization.ValidationError{Field: field, Reason: "must be RFC3339"}
	}
	return &parsed, nil
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

func statusFilter(value string) (*domainreminder.Status, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	status := domainreminder.Status(value)
	switch status {
	case domainreminder.StatusPending, domainreminder.StatusCompleted, domainreminder.StatusCancelled:
		return &status, nil
	default:
		return nil, &serviceauthorization.ValidationError{Field: "status", Reason: "is invalid"}
	}
}

func parseUUID(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if _, err := uuid.Parse(value); err != nil {
		return "", &serviceauthorization.ValidationError{Field: field, Reason: "must be a valid UUID"}
	}
	return value, nil
}

func parseOptionalUUID(value, field string) (*string, error) {
	value, err := parseUUID(value, field)
	if err != nil || value == "" {
		return nil, err
	}
	return &value, nil
}

func toResponse(reminder *domainreminder.Reminder) reminderResponse {
	if reminder == nil {
		return reminderResponse{}
	}
	return reminderResponse{ID: reminder.ID, SpaceID: reminder.SpaceID, Title: reminder.Title, Description: reminder.Description, AssigneeMemberID: reminder.AssigneeMemberID, Status: string(reminder.Status), ScheduledAt: reminder.ScheduledAt.Format(time.RFC3339Nano)}
}
