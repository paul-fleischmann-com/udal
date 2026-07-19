// Package tracing wires OpenTelemetry distributed tracing into the gateway
// (req42.adoc F-24, GitHub issue #29): a trace context is created on every
// incoming request and propagated through auth, routing, and adapter calls
// via ctx, exactly like issue #28's trace_id was already OTEL-TraceID-shaped
// in anticipation of this — logging.Interceptor now reads the real span's
// TraceID instead of generating its own (see internal/logging's
// contextHandler).
package tracing

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TracerName is the instrumentation scope every UDAL-created span (see
// Interceptor, auth.Authenticator's "auth" span, service.DeviceService's
// "router"/"adapter" spans) obtains its tracer from via
// otel.Tracer(TracerName) — one shared name across the whole gateway,
// mirroring how internal/metrics uses one set of package-level collectors
// rather than per-package instrumentation scopes.
const TracerName = "github.com/paulefl/udal/code/gateway"

// NewProvider builds and globally registers the gateway's TracerProvider.
//
// A real *sdktrace.TracerProvider is always built and registered —
// regardless of whether otlpEndpoint is set — because its default sampler
// (ParentBased(AlwaysSample)) and ID generator produce a real, random trace
// ID for every span even with no exporter attached. That's deliberate:
// F-23/issue #28's "every request log line has a trace_id" behavior has to
// keep working when tracing is "disabled" per this AC — disabled here only
// means no OTLP traffic leaves the process, not that request correlation
// stops working. Only when otlpEndpoint is non-empty is a real OTLP gRPC
// exporter wired in via WithBatcher; otherwise the TracerProvider has no
// span processor at all, so spans are created, used for context
// propagation and logging, and then simply discarded — cheap, no network
// I/O, no collector required.
//
// Callers must call Shutdown on the returned provider before the process
// exits, to flush any spans still buffered in the batch processor.
func NewProvider(ctx context.Context, otlpEndpoint, serviceName string) (*sdktrace.TracerProvider, error) {
	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", serviceName)))
	if err != nil {
		return nil, fmt.Errorf("tracing: build resource: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if otlpEndpoint != "" {
		exp, err := newExporter(ctx, otlpEndpoint)
		if err != nil {
			return nil, fmt.Errorf("tracing: create OTLP exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	// W3C Trace Context propagation via gRPC metadata (arc42.adoc §8.3) —
	// not yet exercised by any outbound call this gateway makes (no
	// downstream gRPC fan-out exists today), but registering it is free and
	// is what makes trace context survive a future one.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp, nil
}

// newExporter builds an OTLP/gRPC exporter for endpoint, which may be a
// bare "host:port" (assumed plaintext — the common local-collector case,
// e.g. "otel-collector:4317") or a full URL with scheme
// ("https://collector.example.com:4317").
func newExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	if strings.Contains(endpoint, "://") {
		return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
	}
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
}
