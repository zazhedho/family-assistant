package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"family-assistant/utils"

	"log/slog"
)

const (
	LogLevelPanic = 0
	LogLevelError = 1
	LogLevelFail  = 2
	LogLevelInfo  = 3
	LogLevelData  = 4
	LogLevelWarn  = 5
	LogLevelDebug = 6
)

var logLevelMap = map[int]string{
	LogLevelPanic: "PANIC",
	LogLevelError: "ERROR",
	LogLevelFail:  "FAIL ",
	LogLevelInfo:  "INFO ",
	LogLevelData:  "DATA ",
	LogLevelDebug: "DEBUG",
	LogLevelWarn:  "WARN ",
}

var (
	loggerOnce sync.Once
	appLogger  *slog.Logger
)

type logMetadataKey uint8

const (
	logIDKey logMetadataKey = iota
	userIDKey
)

// WithLogMetadata adds request and user identifiers to a log context.
func WithLogMetadata(ctx context.Context, logID, userID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, logIDKey, logID)
	return context.WithValue(ctx, userIDKey, userID)
}

func WriteLog(level int, msg ...any) {
	writeLog(context.Background(), level, 5, fmt.Sprint(msg...))
}

func WriteLogWithContext(ctx context.Context, level int, msg ...any) {
	writeLog(ctx, level, 6, fmt.Sprint(msg...))
}

func WriteLogWithAttrs(ctx context.Context, level int, msg string, attrs ...slog.Attr) {
	writeLog(ctx, level, 6, msg, attrs...)
}

func writeLog(ctx context.Context, level int, defaultLogLevel int, msg string, extraAttrs ...slog.Attr) {
	if _, ok := logLevelMap[level]; !ok {
		return
	}

	if logLevel, _ := strconv.Atoi(utils.GetEnv("LOG_LEVEL", strconv.Itoa(defaultLogLevel))); logLevel < level {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}

	attrs := []slog.Attr{
		slog.String("server_ip", utils.GetEnv("ServerIP", "")),
	}
	attrs = append(attrs, callerAttrs(3)...)
	if node := utils.GetEnv("NODE", ""); node != "" {
		attrs = append(attrs, slog.String("node", node))
	}

	if logID := LogIDFromContext(ctx); logID != "" {
		attrs = append(attrs, slog.String("log_id", logID))
	}
	if value, ok := contextValue(ctx, "userId"); ok {
		if userID := fmt.Sprint(value); userID != "" {
			attrs = append(attrs, slog.String("user_id", userID))
		}
	}
	attrs = append(attrs, extraAttrs...)

	fields := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		fields = append(fields, attr)
	}

	getLogger().Log(
		ctx,
		mapLevelToSlog(level),
		msg,
		fields...,
	)
}

func contextValue(ctx context.Context, key string) (any, bool) {
	if ctx == nil {
		return nil, false
	}
	if key == utils.CtxKeyId {
		if value := ctx.Value(logIDKey); value != nil {
			return value, true
		}
	}
	if key == "userId" {
		if value := ctx.Value(userIDKey); value != nil {
			return value, true
		}
	}
	if value := ctx.Value(key); value != nil {
		return value, true
	}
	if values, ok := ctx.(interface{ Get(string) (any, bool) }); ok {
		return values.Get(key)
	}
	return nil, false
}

func LogIDFromContext(ctx context.Context) string {
	value, ok := contextValue(ctx, utils.CtxKeyId)
	if !ok {
		return ""
	}
	logID := fmt.Sprint(value)
	if logID == "<nil>" {
		return ""
	}
	return logID
}

func getLogger() *slog.Logger {
	loggerOnce.Do(func() {
		format := utils.NormalizeKey(utils.GetEnv("LOG_FORMAT", "json"))
		options := &slog.HandlerOptions{Level: slog.LevelDebug}

		var handler slog.Handler
		switch format {
		case "text":
			handler = slog.NewTextHandler(os.Stdout, options)
		case "string":
			handler = newStringHandler(os.Stdout, options.Level)
		default:
			handler = slog.NewJSONHandler(os.Stdout, options)
		}

		appLogger = slog.New(handler)
	})

	return appLogger
}

func mapLevelToSlog(level int) slog.Level {
	switch level {
	case LogLevelPanic, LogLevelError:
		return slog.LevelError
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelDebug:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func callerAttrs(skip int) []slog.Attr {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return nil
	}

	sourceFile := normalizeSourceFile(file)
	sourceFunction := ""
	if fn := runtime.FuncForPC(pc); fn != nil {
		sourceFunction = fn.Name()
	}

	return []slog.Attr{
		slog.String("source_file", sourceFile),
		slog.Int("source_line", line),
		slog.String("source_function", sourceFunction),
	}
}

func normalizeSourceFile(file string) string {
	wd, err := os.Getwd()
	if err == nil {
		if rel, relErr := filepath.Rel(wd, file); relErr == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}

	return filepath.ToSlash(file)
}

type stringHandler struct {
	mu     sync.Mutex
	writer io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func newStringHandler(writer io.Writer, level slog.Leveler) slog.Handler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &stringHandler{
		writer: writer,
		level:  level,
	}
}

func (h *stringHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *stringHandler) Handle(_ context.Context, record slog.Record) error {
	fields := map[string]string{}
	addAttr := func(attr slog.Attr) {
		if attr.Key == "" {
			return
		}
		key := attr.Key
		val := attr.Value
		if val.Kind() == slog.KindAny {
			if lv, ok := val.Any().(slog.LogValuer); ok {
				val = lv.LogValue()
			}
		}
		if len(h.groups) > 0 {
			key = strings.Join(h.groups, ".") + "." + key
		}
		switch val.Kind() {
		case slog.KindString:
			fields[key] = val.String()
		default:
			fields[key] = fmt.Sprint(val.Any())
		}
	}

	for _, attr := range h.attrs {
		addAttr(attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addAttr(attr)
		return true
	})

	level := strings.ToUpper(record.Level.String())
	prefix := fmt.Appendf(nil, "[%s][%s][%s]", fields["server_ip"], fields["node"], level)
	if logID := fields["log_id"]; logID != "" {
		prefix = fmt.Appendf(prefix, "[%s]", logID)
	}
	if userID := fields["user_id"]; userID != "" {
		prefix = fmt.Appendf(prefix, "[%s]", userID)
	}
	if sourceFile := fields["source_file"]; sourceFile != "" {
		prefix = fmt.Appendf(prefix, "[%s:%s]", sourceFile, fields["source_line"])
	}
	fieldKeys := make([]string, 0, len(fields))
	for key := range fields {
		switch key {
		case "server_ip", "node", "log_id", "user_id", "source_file", "source_line", "source_function":
			continue
		}
		fieldKeys = append(fieldKeys, key)
	}
	sort.Strings(fieldKeys)
	for _, key := range fieldKeys {
		prefix = fmt.Appendf(prefix, "[%s=%s]", key, fields[key])
	}

	ts := record.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	line := fmt.Sprintf("%s %s %s\n", ts.Format("2006/01/02 15:04:05 .000000"), prefix, record.Message)

	h.mu.Lock()
	_, err := io.WriteString(h.writer, line)
	h.mu.Unlock()
	return err
}

func (h *stringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &stringHandler{
		writer: h.writer,
		level:  h.level,
		attrs:  append(append([]slog.Attr{}, h.attrs...), attrs...),
		groups: append([]string{}, h.groups...),
	}
}

func (h *stringHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &stringHandler{
		writer: h.writer,
		level:  h.level,
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: append(append([]string{}, h.groups...), name),
	}
}
