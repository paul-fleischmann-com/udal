package tracing

import (
	"context"
	"testing"
)

// TestNewProvider_NoEndpoint covers F-24 AC: "tracing disabled if unset" —
// NewProvider must still succeed and return a usable provider with no OTLP
// endpoint configured (no exporter, no network I/O), since spans still need
// to produce real trace IDs for request-log correlation (issue #28).
func TestNewProvider_NoEndpoint(t *testing.T) {
	tp, err := NewProvider(context.Background(), "", "udal-gateway-test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span := tp.Tracer(TracerName).Start(context.Background(), "test-span")
	defer span.End()
	if !span.SpanContext().HasTraceID() {
		t.Error("span has no trace ID even without an OTLP endpoint configured — request-log correlation would break")
	}
}

// TestNewProvider_BareHostPort covers the "host:port" (no scheme) endpoint
// form, e.g. UDAL_OTEL_ENDPOINT=otel-collector:4317 — must not error just
// building the exporter (no real collector needs to be reachable; the
// exporter connects lazily).
func TestNewProvider_BareHostPort(t *testing.T) {
	tp, err := NewProvider(context.Background(), "127.0.0.1:0", "udal-gateway-test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	_ = tp.Shutdown(context.Background())
}

// TestNewProvider_URLEndpoint covers the full-URL endpoint form.
func TestNewProvider_URLEndpoint(t *testing.T) {
	tp, err := NewProvider(context.Background(), "https://127.0.0.1:0", "udal-gateway-test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	_ = tp.Shutdown(context.Background())
}

// TestNewProvider_DifferentSpansShareNoTraceID sanity-checks that two
// independently-started (non-parented) spans get *different* trace IDs —
// guards against an accidentally-fixed/zero ID generator.
func TestNewProvider_DifferentSpansShareNoTraceID(t *testing.T) {
	tp, err := NewProvider(context.Background(), "", "udal-gateway-test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span1 := tp.Tracer(TracerName).Start(context.Background(), "a")
	_, span2 := tp.Tracer(TracerName).Start(context.Background(), "b")
	span1.End()
	span2.End()

	if span1.SpanContext().TraceID() == span2.SpanContext().TraceID() {
		t.Error("two unrelated root spans got the same trace ID")
	}
}
