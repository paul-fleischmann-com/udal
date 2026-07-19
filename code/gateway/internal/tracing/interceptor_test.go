package tracing

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newTestProvider builds a TracerProvider that exports synchronously into
// an in-memory recorder — lets tests assert on exactly which spans were
// created, without a real OTLP collector.
func newTestProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
	})
	return tp, exp
}

func TestInterceptor_UnaryInterceptor_StartsAPISpan(t *testing.T) {
	_, exp := newTestProvider(t)
	var i Interceptor

	var sawSpanInHandler bool
	handler := func(ctx context.Context, req any) (any, error) {
		sawSpanInHandler = trace.SpanContextFromContext(ctx).HasTraceID()
		return "ok", nil
	}
	_, err := i.UnaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/x/Test"}, handler)
	if err != nil {
		t.Fatalf("UnaryInterceptor: %v", err)
	}
	if !sawSpanInHandler {
		t.Fatal("handler's context has no active span")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Name != "api" {
		t.Fatalf("spans = %+v, want exactly one named \"api\"", spans)
	}
}

func TestInterceptor_UnaryInterceptor_RecordsErrorStatus(t *testing.T) {
	_, exp := newTestProvider(t)
	var i Interceptor

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.PermissionDenied, "nope")
	}
	_, _ = i.UnaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/x/TestErr"}, handler)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Status.Code.String() != "Error" {
		t.Errorf("span status = %v, want Error", spans[0].Status.Code)
	}
}

// fakeServerStream is a minimal grpc.ServerStream for testing
// StreamInterceptor without a real gRPC connection.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeServerStream) Context() context.Context { return s.ctx }

func TestInterceptor_StreamInterceptor_WrapsContextWithSpan(t *testing.T) {
	newTestProvider(t)
	var i Interceptor

	var sawSpan bool
	handler := func(srv any, ss grpc.ServerStream) error {
		sawSpan = trace.SpanContextFromContext(ss.Context()).HasTraceID()
		return nil
	}
	err := i.StreamInterceptor(nil, &fakeServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{FullMethod: "/x/TestStream"}, handler)
	if err != nil {
		t.Fatalf("StreamInterceptor: %v", err)
	}
	if !sawSpan {
		t.Fatal("stream's context has no active span")
	}
}

func TestInterceptor_NonStatusError(t *testing.T) {
	_, exp := newTestProvider(t)
	var i Interceptor

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, errors.New("boom")
	}
	_, _ = i.UnaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/x/TestBoom"}, handler)

	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code.String() != "Error" {
		t.Fatalf("spans = %+v, want one Error-status span", spans)
	}
}
