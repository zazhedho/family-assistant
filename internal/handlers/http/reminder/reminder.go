package handlerreminder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zazhedho/family-assistant/internal/authscope"
	domainreminder "github.com/zazhedho/family-assistant/internal/domain/reminder"
	serviceauthorization "github.com/zazhedho/family-assistant/internal/services/authorization"
	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
	servicereminder "github.com/zazhedho/family-assistant/internal/services/reminder"
	"github.com/zazhedho/family-assistant/pkg/response"
	"github.com/zazhedho/family-assistant/utils"
	"gorm.io/gorm"
)

type ReminderHandler struct {
	Service  servicereminder.Service
	Resolver serviceidentity.UserResolver
}

type actorContextKey struct{}

func withActorContext(ctx context.Context, actor serviceidentity.ActorContext) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func actorFromContext(ctx context.Context) (serviceidentity.ActorContext, bool) {
	if ctx == nil {
		return serviceidentity.ActorContext{}, false
	}
	actor, ok := ctx.Value(actorContextKey{}).(serviceidentity.ActorContext)
	return actor, ok
}

func NewReminderHandler(service servicereminder.Service, resolver serviceidentity.UserResolver) *ReminderHandler {
	return &ReminderHandler{Service: service, Resolver: resolver}
}

func FamilyPermissionMiddleware(resolver serviceidentity.UserResolver, permission string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		scope := authscope.FromContext(ctx.Request.Context())
		userID := strings.TrimSpace(scope.UserID)
		if userID == "" {
			writeReminderError(ctx, serviceidentity.ErrUnauthenticated)
			ctx.Abort()
			return
		}
		if resolver == nil {
			writeReminderError(ctx, errors.New("user resolver is not configured"))
			ctx.Abort()
			return
		}
		actor, err := resolver.ResolveUser(ctx.Request.Context(), userID, "http")
		if err != nil {
			writeReminderError(ctx, err)
			ctx.Abort()
			return
		}
		if !actor.HasPermission(permission) {
			writeReminderError(ctx, serviceauthorization.ErrForbidden)
			ctx.Abort()
			return
		}
		actor.InitiatorUserID = ""
		actor.InitiatorRoleName = ""
		if scope.IsImpersonated {
			actor.InitiatorUserID = strings.TrimSpace(scope.OriginalUserID)
			actor.InitiatorRoleName = strings.TrimSpace(scope.OriginalRole)
		}
		ctx.Request = ctx.Request.WithContext(withActorContext(ctx.Request.Context(), actor))
		ctx.Next()
	}
}

type createRequest struct {
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	ScheduledAt    string `json:"scheduled_at"`
	Scope          string `json:"scope,omitempty"`
	TargetMemberID string `json:"target_member_id,omitempty"`
}

type reminderResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
	ScheduledAt string `json:"scheduled_at"`
}

func (h *ReminderHandler) Create(ctx *gin.Context) {
	actor, err := h.actor(ctx)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	var request createRequest
	if err := decodeSingleJSON(ctx, &request); err != nil {
		h.writeError(ctx, &serviceauthorization.ValidationError{Field: "body", Reason: "is invalid"})
		return
	}
	scheduledAt, err := parseHTTPTime(request.ScheduledAt, "scheduled_at", true)
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
		Scope: domainreminder.Scope(strings.TrimSpace(request.Scope)), TargetMemberID: optionalID(request.TargetMemberID),
	})
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, response.Response(http.StatusCreated, "Reminder created successfully", utils.GenerateLogId(ctx), toResponse(reminder)))
}

func (h *ReminderHandler) List(ctx *gin.Context) {
	actor, err := h.actor(ctx)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	input := servicereminder.ListInput{
		Scope: scopeFilter(ctx.Query("scope")), Status: statusFilter(ctx.Query("status")), TargetMemberID: optionalID(ctx.Query("target_member_id")),
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
	actor, err := h.actor(ctx)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	if h.Service == nil {
		h.writeError(ctx, errors.New("reminder service is not configured"))
		return
	}
	reminder, err := h.Service.Complete(ctx.Request.Context(), actor, ctx.Param("id"))
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, response.Response(http.StatusOK, "Reminder completed successfully", utils.GenerateLogId(ctx), toResponse(reminder)))
}

func (h *ReminderHandler) actor(ctx *gin.Context) (serviceidentity.ActorContext, error) {
	if actor, ok := actorFromContext(ctx.Request.Context()); ok {
		return actor, nil
	}
	scope := authscope.FromContext(ctx.Request.Context())
	userID := strings.TrimSpace(scope.UserID)
	if userID == "" {
		return serviceidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	if h.Resolver == nil {
		return serviceidentity.ActorContext{}, errors.New("user resolver is not configured")
	}
	return h.Resolver.ResolveUser(ctx.Request.Context(), userID, "http")
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

func optionalID(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func scopeFilter(value string) *domainreminder.Scope {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	scope := domainreminder.Scope(value)
	return &scope
}

func statusFilter(value string) *domainreminder.Status {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	status := domainreminder.Status(value)
	return &status
}

func toResponse(reminder *domainreminder.Reminder) reminderResponse {
	if reminder == nil {
		return reminderResponse{}
	}
	return reminderResponse{ID: reminder.ID, Title: reminder.Title, Scope: string(reminder.Scope), Status: string(reminder.Status), ScheduledAt: reminder.ScheduledAt.Format(time.RFC3339Nano)}
}
