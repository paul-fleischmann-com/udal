package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestInterceptor(buf *bytes.Buffer) *Interceptor {
	log := slog.New(NewHandler(buf, &slog.LevelVar{})).With("component", "gateway.api")
	return &Interceptor{Log: log}
}

func TestInterceptor_UnaryInterceptor_LogsOneLineWithTraceID(t *testing.T) {
	var buf bytes.Buffer
	i := newTestInterceptor(&buf)

	var sawTraceID string
	handler := func(ctx context.Context, req any) (any, error) {
		id, ok := TraceIDFromContext(ctx)
		if !ok {
			t.Error("handler's context has no trace ID")
		}
		sawTraceID = id
		return "response", nil
	}

	resp, err := i.UnaryInterceptor(context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/udal.v1.DeviceService/GetProperty"}, handler)
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
	if m["trace_id"] != sawTraceID {
		t.Errorf(`logged "trace_id" = %v, want %q (the one the handler observed)`, m["trace_id"], sawTraceID)
	}
	if len(sawTraceID) != 32 {
		t.Errorf("trace ID = %q, want 32 hex chars", sawTraceID)
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

func TestInterceptor_StreamInterceptor_WrapsContextWithTraceID(t *testing.T) {
	var buf bytes.Buffer
	i := newTestInterceptor(&buf)

	var sawTraceID string
	handler := func(srv any, ss grpc.ServerStream) error {
		id, ok := TraceIDFromContext(ss.Context())
		if !ok {
			t.Error("stream's context has no trace ID")
		}
		sawTraceID = id
		return nil
	}

	err := i.StreamInterceptor(nil, &fakeServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{FullMethod: "/udal.v1.DeviceService/Subscribe"}, handler)
	if err != nil {
		t.Fatalf("StreamInterceptor: %v", err)
	}

	m := decodeLine(t, &buf)
	if m["method"] != "/udal.v1.DeviceService/Subscribe" {
		t.Errorf(`"method" = %v`, m["method"])
	}
	if m["trace_id"] != sawTraceID || len(sawTraceID) != 32 {
		t.Errorf("trace_id mismatch: logged=%v, handler saw=%q", m["trace_id"], sawTraceID)
	}
}
