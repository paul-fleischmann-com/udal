package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type traceIDCtxKey struct{}

// GenerateTraceID returns a new 32-hex-character trace ID: 16 random
// bytes, matching the W3C Trace Context / OpenTelemetry TraceID format.
// Deliberately so — F-23's AC asks a request's logged trace_id to match
// "the OTEL span ID", but OpenTelemetry tracing itself isn't wired in yet
// (issue #29 / F-24). Generating an already OTEL-shaped ID now means that
// work only has to replace this call with the real span's TraceID; no
// change to the log format or any consumer of trace_id.
func GenerateTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read on a supported platform practically never fails; a zero ID is still valid hex, just uncorrelatable
	return hex.EncodeToString(b[:])
}

// WithTraceID returns a context carrying id, retrievable via
// TraceIDFromContext — used by Interceptor to establish a per-request
// trace ID, and by contextHandler (handler.go) to attach it to every log
// line made with that context (via the *Context slog.Logger methods).
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDCtxKey{}, id)
}

// TraceIDFromContext retrieves the trace ID set by WithTraceID, if any.
func TraceIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(traceIDCtxKey{}).(string)
	return id, ok
}
