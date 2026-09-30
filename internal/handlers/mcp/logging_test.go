package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	"family-assistant/pkg/config"
	"family-assistant/pkg/logger"

	"github.com/google/uuid"
)

func TestMCPToolLogAttrsIncludeSafeFieldsOnly(t *testing.T) {
	attrs := mcpToolLogAttrs(
		"activity_create_with_reminder",
		ActivityCreateWithReminderInput{
			Activity: ActivityCreateInput{
				Kind: "feeding", Note: "private activity note", OccurredAt: "2026-09-29T10:00:00+07:00",
			},
			Reminder: ActivityReminderInput{
				Title: "private reminder title", Description: "private reminder description",
				ScheduledAt: "2026-09-29T10:30:00+07:00", AssigneeMemberID: "member-1",
			},
		},
		ActivityCreateWithReminderOutput{
			Status: "created",
			Activity: &domainactivity.Activity{
				ID: "activity-1", SpaceID: "space-1", Kind: "feeding", Note: "private activity note",
				OccurredAt: time.Date(2026, 9, 29, 10, 0, 0, 0, time.FixedZone("UTC+07", 7*60*60)),
			},
			Reminder: &ReminderOutput{
				ID: "reminder-1", SpaceID: "space-1", Title: "private reminder title",
				Description: "private reminder description", Status: "PENDING", ScheduledAt: "2026-09-29T10:30:00+07:00",
			},
		},
		123*time.Millisecond,
		nil,
	)

	got := mcpLogAttrValues(attrs)
	for key, want := range map[string]string{
		"tool_name":                   "activity_create_with_reminder",
		"call_status":                 "success",
		"duration_ms":                 "123",
		"output_status":               "created",
		"input_activity_kind":         "feeding",
		"input_activity_occurred_at":  "2026-09-29T10:00:00+07:00",
		"input_reminder_scheduled_at": "2026-09-29T10:30:00+07:00",
		"output_activity_id":          "activity-1",
		"output_activity_space_id":    "space-1",
		"output_reminder_id":          "reminder-1",
		"output_reminder_status":      "PENDING",
	} {
		if got[key] != want {
			t.Errorf("attribute %q = %q, want %q; all attrs: %+v", key, got[key], want, got)
		}
	}
	for _, value := range got {
		if strings.Contains(value, "private") {
			t.Fatalf("log attrs contain private content: %+v", got)
		}
	}
}

func TestMCPToolLogAttrsDoNotExposeErrorMessages(t *testing.T) {
	attrs := mcpToolLogAttrs("activity_create", nil, nil, time.Millisecond, errors.New("private persistence detail"))
	got := mcpLogAttrValues(attrs)
	if got["call_status"] != "error" || got["error_type"] == "" {
		t.Fatalf("expected safe error metadata, got %+v", got)
	}
	if strings.Contains(fmt.Sprint(got), "private persistence detail") {
		t.Fatalf("log attrs expose error message: %+v", got)
	}
}

func TestMCPAuthMiddlewarePreservesOrCreatesLogID(t *testing.T) {
	for _, tt := range []struct {
		name        string
		logID       string
		shouldReuse bool
	}{
		{name: "reuse existing id", logID: "request-123", shouldReuse: true},
		{name: "create id when absent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.shouldReuse {
				ctx = logger.WithLogMetadata(ctx, tt.logID, "")
			}
			var got string
			next := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				got = logger.LogIDFromContext(request.Context())
			})
			handler := NewAuthMiddleware(config.MCPConfig{ServerKey: "mcp-key"}).Handler(next)
			request := httptest.NewRequest(http.MethodPost, "/mcp", nil).WithContext(ctx)
			request.Header.Set("Authorization", "Bearer mcp-key")
			handler.ServeHTTP(httptest.NewRecorder(), request)

			if got == "" {
				t.Fatal("expected middleware to propagate a log ID")
			}
			if tt.shouldReuse {
				if got != tt.logID {
					t.Fatalf("log ID = %q, want existing %q", got, tt.logID)
				}
				return
			}
			id, err := uuid.Parse(got)
			if err != nil {
				t.Fatalf("generated log ID %q is not a UUID: %v", got, err)
			}
			if version := id.Version(); version != 7 {
				t.Fatalf("generated log ID version = %d, want UUIDv7 from utils.CreateUUID", version)
			}
		})
	}
}

func mcpLogAttrValues(attrs []slog.Attr) map[string]string {
	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		value := attr.Value.Resolve()
		if value.Kind() == slog.KindString {
			values[attr.Key] = value.String()
			continue
		}
		values[attr.Key] = fmt.Sprint(value.Any())
	}
	return values
}
