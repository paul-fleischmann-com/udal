package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log line written")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, line)
	}
	return m
}

// TestHandler_MandatoryFields covers F-23 AC: "Every log line is valid
// JSON with all mandatory fields" (timestamp, level, component — trace_id
// is covered separately, see TestHandler_TraceIDFromContext, since it's
// only present when the call is scoped to a request).
func TestHandler_MandatoryFields(t *testing.T) {
	var buf bytes.Buffer
	levelVar := &slog.LevelVar{}
	log := slog.New(NewHandler(&buf, levelVar)).With("component", "gateway")

	log.Info("hello")

	m := decodeLine(t, &buf)
	if _, ok := m["timestamp"]; !ok {
		t.Errorf("log line missing %q field: %v", "timestamp", m)
	}
	if _, ok := m["time"]; ok {
		t.Errorf("log line has slog's default %q key, want it renamed to %q: %v", "time", "timestamp", m)
	}
	if m["level"] != "INFO" {
		t.Errorf(`log line "level" = %v, want "INFO"`, m["level"])
	}
	if m["component"] != "gateway" {
		t.Errorf(`log line "component" = %v, want "gateway"`, m["component"])
	}
	if m["msg"] != "hello" {
		t.Errorf(`log line "msg" = %v, want "hello"`, m["msg"])
	}
}

// TestHandler_TraceIDFromContext covers F-23 AC: "Request log line
// includes trace_id" — a log call made with a context carrying a trace ID
// (via WithTraceID, e.g. from Interceptor) gets it attached automatically,
// without the call site passing it explicitly.
func TestHandler_TraceIDFromContext(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, &slog.LevelVar{}))

	ctx := WithTraceID(context.Background(), "deadbeefdeadbeefdeadbeefdeadbeef")
	log.InfoContext(ctx, "request")

	m := decodeLine(t, &buf)
	if m["trace_id"] != "deadbeefdeadbeefdeadbeefdeadbeef" {
		t.Errorf(`log line "trace_id" = %v, want the context's trace ID`, m["trace_id"])
	}
}

// TestHandler_NoTraceIDWithoutContext ensures a plain (non-request-scoped)
// log call doesn't get a spurious trace_id field — only calls whose
// context actually carries one should have it.
func TestHandler_NoTraceIDWithoutContext(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, &slog.LevelVar{}))

	log.Info("startup message")

	m := decodeLine(t, &buf)
	if _, ok := m["trace_id"]; ok {
		t.Errorf(`log line unexpectedly has "trace_id": %v`, m)
	}
}

// TestHandler_TraceIDSurvivesWith verifies the contextHandler's WithAttrs
// wrapping (needed for component-scoped child loggers, see
// cmd/gateway/main.go) doesn't drop the context-trace-ID behavior.
func TestHandler_TraceIDSurvivesWith(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, &slog.LevelVar{})).With("component", "mqtt_adapter")

	ctx := WithTraceID(context.Background(), "cafebabecafebabecafebabecafebabe")
	log.InfoContext(ctx, "watch device")

	m := decodeLine(t, &buf)
	if m["component"] != "mqtt_adapter" {
		t.Errorf(`log line "component" = %v, want "mqtt_adapter"`, m["component"])
	}
	if m["trace_id"] != "cafebabecafebabecafebabecafebabe" {
		t.Errorf(`log line "trace_id" = %v, want the context's trace ID`, m["trace_id"])
	}
}

// TestHandler_RespectsLevel verifies the *slog.LevelVar passed to
// NewHandler actually gates output — required for LevelHandler's runtime
// adjustment (debug_handler.go) to have any effect.
func TestHandler_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	levelVar := &slog.LevelVar{}
	levelVar.Set(slog.LevelWarn)
	log := slog.New(NewHandler(&buf, levelVar))

	log.Info("should be suppressed")
	if buf.Len() != 0 {
		t.Fatalf("Info logged at Warn level: %q", buf.String())
	}

	levelVar.Set(slog.LevelDebug)
	log.Debug("should now appear")
	if buf.Len() == 0 {
		t.Fatal("Debug not logged after raising level to Debug")
	}
}
