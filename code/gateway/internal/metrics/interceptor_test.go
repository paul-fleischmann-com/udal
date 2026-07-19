package metrics

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOperationName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/udal.v1.DeviceService/GetProperty", "GetProperty"},
		{"GetProperty", "GetProperty"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := operationName(tt.in); got != tt.want {
			t.Errorf("operationName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestInterceptor_UnaryInterceptor_RecordsMetrics(t *testing.T) {
	var i Interceptor
	handler := func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	}
	_, err := i.UnaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/udal.v1.DeviceService/TestOp"}, handler)
	if err != nil {
		t.Fatalf("UnaryInterceptor: %v", err)
	}

	count := testutil.ToFloat64(Requests.WithLabelValues("TestOp", codes.OK.String()))
	if count < 1 {
		t.Errorf("Requests{operation=TestOp,status=OK} = %v, want >= 1", count)
	}

	samples := testutil.CollectAndCount(RequestDuration, "udal_request_duration_seconds")
	if samples == 0 {
		t.Error("RequestDuration has no samples after a recorded request")
	}
}

func TestInterceptor_UnaryInterceptor_RecordsErrorStatus(t *testing.T) {
	var i Interceptor
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.NotFound, "nope")
	}
	_, _ = i.UnaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/x/TestOpNotFound"}, handler)

	count := testutil.ToFloat64(Requests.WithLabelValues("TestOpNotFound", codes.NotFound.String()))
	if count < 1 {
		t.Errorf("Requests{operation=TestOpNotFound,status=NotFound} = %v, want >= 1", count)
	}
}

func TestInterceptor_UnaryInterceptor_NonStatusErrorRecordsUnknown(t *testing.T) {
	var i Interceptor
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, errors.New("boom")
	}
	_, _ = i.UnaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/x/TestOpBoom"}, handler)

	count := testutil.ToFloat64(Requests.WithLabelValues("TestOpBoom", codes.Unknown.String()))
	if count < 1 {
		t.Errorf("Requests{operation=TestOpBoom,status=Unknown} = %v, want >= 1", count)
	}
}

// fakeServerStream is a minimal grpc.ServerStream for testing
// StreamInterceptor without a real gRPC connection.
type fakeServerStream struct {
	grpc.ServerStream
}

func (fakeServerStream) Context() context.Context { return context.Background() }

func TestInterceptor_StreamInterceptor_RecordsMetrics(t *testing.T) {
	var i Interceptor
	handler := func(srv any, ss grpc.ServerStream) error { return nil }

	err := i.StreamInterceptor(nil, fakeServerStream{}, &grpc.StreamServerInfo{FullMethod: "/x/TestStreamOp"}, handler)
	if err != nil {
		t.Fatalf("StreamInterceptor: %v", err)
	}

	count := testutil.ToFloat64(Requests.WithLabelValues("TestStreamOp", codes.OK.String()))
	if count < 1 {
		t.Errorf("Requests{operation=TestStreamOp,status=OK} = %v, want >= 1", count)
	}
}

// TestInterceptor_StreamInterceptor_DoesNotRecordDuration covers a fix from
// review: StreamInterceptor must not observe RequestDuration for streaming
// RPCs — a long-lived stream (DeviceService.StreamCommands can stay open
// for a device's whole session) timed start-to-close would always land in
// prometheus.DefBuckets' +Inf overflow bucket, a useless/misleading signal.
func TestInterceptor_StreamInterceptor_DoesNotRecordDuration(t *testing.T) {
	var i Interceptor
	handler := func(srv any, ss grpc.ServerStream) error { return nil }

	before := testutil.CollectAndCount(RequestDuration, "udal_request_duration_seconds")
	if err := i.StreamInterceptor(nil, fakeServerStream{}, &grpc.StreamServerInfo{FullMethod: "/x/TestStreamOpNoDuration"}, handler); err != nil {
		t.Fatalf("StreamInterceptor: %v", err)
	}
	after := testutil.CollectAndCount(RequestDuration, "udal_request_duration_seconds")

	if after != before {
		t.Errorf("RequestDuration sample count = %d, want unchanged %d (streams must not observe duration)", after, before)
	}
}
