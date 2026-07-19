package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestInterceptor(buf *bytes.Buffer) *Interceptor {
	log := slog.New(NewHandler(buf, &slog.LevelVar{})).With("component", "gateway.api")
	return &Interceptor{Log: log}
}

// withTestSpan simulates what tracing.Interceptor (issue #29) does in
// production — establishing a real OTel span in ctx — without depending on
// that package (would be an import cycle: internal/tracing doesn't import
// internal/logging, but internal/logging is lower-level and shouldn't
// import internal/tracing's Interceptor either; a local, throwaway
// TracerProvider is simpler and just as faithful for this test's purpose).
func withTestSpan(ctx context.Context) (context.Context, trace.TraceID) {
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(ctx, "test-span")
	return ctx, span.SpanContext().TraceID()
}

// TestInterceptor_UnaryInterceptor_LogsOneLineWithTraceID covers F-23 AC:
// "Request log line includes trace_id" — once issue #29's tracing
// interceptor has established a real span (simulated here via
// withTestSpan), Interceptor's request-summary line carries that same
// trace ID, read automatically by contextHandler (handler.go).
func TestInterceptor_UnaryInterceptor_LogsOneLineWithTraceID(t *testing.T) {
	var buf bytes.Buffer
	i := newTestInterceptor(&buf)
	ctx, wantTraceID := withTestSpan(context.Background())

	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	resp, err := i.UnaryInterceptor(ctx, "request", &grpc.UnaryServerInfo{FullMethod: "/udal.v1.DeviceService/GetProperty"}, handler)
	if err != nil {
		t.Fatalf("UnaryInterceptor: %v", err)
	}
	if resp != "response" {
		t.Errorf("resp = %v, want %q", resp, "response")
	}

	m := decodeLine(t, &buf)
	if m["msg"] != "request" {
		t.Errorf(`"msg" = %v, want "request"`, m["msg"])
	}
	if m["method"] != "/udal.v1.DeviceService/GetProperty" {
		t.Errorf(`"method" = %v`, m["method"])
	}
	if m["code"] != codes.OK.String() {
		t.Errorf(`"code" = %v, want %q`, m["code"], codes.OK.String())
	}
	if m["trace_id"] != wantTraceID.String() {
		t.Errorf(`logged "trace_id" = %v, want %q (the active span's trace ID)`, m["trace_id"], wantTraceID.String())
	}
}

// TestInterceptor_UnaryInterceptor_NoActiveSpanLogsNoTraceID documents that
// Interceptor no longer generates a trace ID itself (issue #29 moved that
// responsibility to tracing.Interceptor, which must run first in the real
// chain — see cmd/gateway/main.go) — a request with no active span logs
// with no trace_id field at all, same as any other non-request-scoped log
// call (handler_test.go's TestHandler_NoTraceIDWithoutContext).
func TestInterceptor_UnaryInterceptor_NoActiveSpanLogsNoTraceID(t *testing.T) {
	var buf bytes.Buffer
	i := newTestInterceptor(&buf)

	handler := func(ctx context.Context, req any) (any, error) { return "response", nil }
	if _, err := i.UnaryInterceptor(context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/x"}, handler); err != nil {
		t.Fatalf("UnaryInterceptor: %v", err)
	}

	m := decodeLine(t, &buf)
	if _, ok := m["trace_id"]; ok {
		t.Errorf(`log line unexpectedly has "trace_id": %v`, m)
	}
}

func TestInterceptor_UnaryInterceptor_LogsErrorCode(t *testing.T) {
	var buf bytes.Buffer
	i := newTestInterceptor(&buf)

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.NotFound, "device not found")
	}

	_, err := i.UnaryInterceptor(context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/udal.v1.DeviceService/GetDevice"}, handler)
	if err == nil {
		t.Fatal("UnaryInterceptor: want error, got nil")
	}

	m := decodeLine(t, &buf)
	if m["code"] != codes.NotFound.String() {
		t.Errorf(`"code" = %v, want %q`, m["code"], codes.NotFound.String())
	}
}

func TestInterceptor_UnaryInterceptor_NonStatusErrorLogsUnknown(t *testing.T) {
	var buf bytes.Buffer
	i := newTestInterceptor(&buf)

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, errors.New("boom")
	}

	_, _ = i.UnaryInterceptor(context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/x"}, handler)

	m := decodeLine(t, &buf)
	if m["code"] != codes.Unknown.String() {
		t.Errorf(`"code" = %v, want %q (grpc status.Code's fallback for a non-status error)`, m["code"], codes.Unknown.String())
	}
}

// fakeServerStream is a minimal grpc.ServerStream for testing
// StreamInterceptor without a real gRPC connection.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeServerStream) Context() context.Context { return s.ctx }

func TestInterceptor_StreamInterceptor_LogsWithTraceID(t *testing.T) {
	var buf bytes.Buffer
	i := newTestInterceptor(&buf)
	ctx, wantTraceID := withTestSpan(context.Background())

	handler := func(srv any, ss grpc.ServerStream) error { return nil }

	err := i.StreamInterceptor(nil, &fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/udal.v1.DeviceService/Subscribe"}, handler)
	if err != nil {
		t.Fatalf("StreamInterceptor: %v", err)
	}

	m := decodeLine(t, &buf)
	if m["method"] != "/udal.v1.DeviceService/Subscribe" {
		t.Errorf(`"method" = %v`, m["method"])
	}
	if m["trace_id"] != wantTraceID.String() {
		t.Errorf(`logged "trace_id" = %v, want %q`, m["trace_id"], wantTraceID.String())
	}
}
