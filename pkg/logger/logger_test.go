package logger

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"family-assistant/utils"
)

func TestMapLevelToSlog(t *testing.T) {
	if got := mapLevelToSlog(LogLevelError); got != slog.LevelError {
		t.Fatalf("expected error level, got %v", got)
	}
	if got := mapLevelToSlog(LogLevelWarn); got != slog.LevelWarn {
		t.Fatalf("expected warn level, got %v", got)
	}
	if got := mapLevelToSlog(LogLevelDebug); got != slog.LevelDebug {
		t.Fatalf("expected debug level, got %v", got)
	}
	if got := mapLevelToSlog(LogLevelInfo); got != slog.LevelInfo {
		t.Fatalf("expected info level, got %v", got)
	}
}

func TestStringHandlerWritesCompactLine(t *testing.T) {
	var buf bytes.Buffer
	handler := newStringHandler(&buf, slog.LevelDebug)
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello", 0)
	record.AddAttrs(
		slog.String("server_ip", "127.0.0.1"),
		slog.String("node", "api"),
		slog.String("source_file", "logger_test.go"),
		slog.Int("source_line", 10),
	)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[127.0.0.1][api][INFO]") || !strings.Contains(out, "hello") {
		t.Fatalf("unexpected log line: %q", out)
	}
}

func TestStringHandlerWithGroupAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	handler := newStringHandler(&buf, slog.LevelDebug).WithGroup("request").WithAttrs([]slog.Attr{slog.String("log_id", "log-1")})
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected info level enabled")
	}

	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello", 0)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestLoggerPublicWritePaths(t *testing.T) {
	t.Setenv("LOG_LEVEL", "6")
	t.Setenv("LOG_FORMAT", "string")
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = writer.Close()
		_ = reader.Close()
	})

	WriteLog(999, "ignored")
	WriteLog(LogLevelDebug, "debug message")

	ctx := WithLogMetadata(context.Background(), "request-1", "user-1")
	WriteLogWithContext(ctx, LogLevelInfo, "context message")
	WriteLogWithAttrs(ctx, LogLevelInfo, "structured message", slog.String("activity_id", "activity-1"))

	gin.SetMode(gin.TestMode)
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest("GET", "/", nil)
	ginContext.Set(utils.CtxKeyId, uuid.MustParse("d7be086d-c27b-4fc7-a43e-424acb52c045"))
	ginContext.Set("userId", "gin-user")
	WriteLogWithContext(ginContext, LogLevelInfo, "gin context message")

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	os.Stdout = originalStdout
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured logs: %v", err)
	}
	for _, want := range []string{
		"[request-1][user-1]",
		"activity_id=activity-1",
		"[d7be086d-c27b-4fc7-a43e-424acb52c045][gin-user]",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("expected log output to contain %q, got %q", want, output)
		}
	}

	if attrs := callerAttrs(0); len(attrs) == 0 {
		t.Fatal("expected caller attrs")
	}
	if got := normalizeSourceFile("pkg/logger/logger.go"); got == "" {
		t.Fatal("expected normalized source file")
	}
	if logger := getLogger(); logger == nil {
		t.Fatal("expected logger singleton")
	}
}
