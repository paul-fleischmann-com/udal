package logging

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// NewHandler returns the gateway's structured JSON log handler: slog's own
// JSON handler with its default "time" key renamed to "timestamp" (F-23's
// mandated field name — "level" and slog's default casing need no
// change), wrapped so any log call made with a context carrying a trace ID
// (see trace.go, Interceptor) gets a "trace_id" attribute attached
// automatically, without every call site across the gateway needing to add
// one by hand. "component" isn't handled here — callers derive a
// component-scoped child logger via (*slog.Logger).With("component", …)
// once per subsystem (see cmd/gateway/main.go), the standard slog pattern
// for a fixed per-logger attribute.
func NewHandler(w io.Writer, level slog.Leveler) slog.Handler {
	jsonHandler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceAttr,
	})
	return contextHandler{jsonHandler}
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		a.Key = "timestamp"
	}
	return a
}

// contextHandler wraps a slog.Handler to append a "trace_id" attribute
// from ctx to every log record that has one — the standard documented
// pattern for attaching a context-scoped value to every log.Handler.Handle
// call without threading it through every log call's explicit arguments.
//
// The real OpenTelemetry span in ctx (tracing.Interceptor establishes one
// for every request, issue #29) is the primary source — this is what makes
// F-23's "Request log line includes trace_id matching the OTEL span ID"
// (issue #28) literally true, not just OTEL-ID-shaped. WithTraceID/
// TraceIDFromContext remain as a fallback for a context with no active
// span (e.g. a call site that predates or bypasses the tracing
// interceptor, such as a background goroutine).
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
	} else if traceID, ok := TraceIDFromContext(ctx); ok {
		r.AddAttrs(slog.String("trace_id", traceID))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
