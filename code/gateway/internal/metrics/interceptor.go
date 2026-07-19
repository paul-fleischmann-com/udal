package metrics

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Interceptor records Requests and RequestDuration for every gRPC request
// (also covering REST, proxied through the same server). Chain it
// alongside logging.Interceptor in cmd/gateway/main.go — order between the
// two doesn't matter, neither depends on the other's context changes.
type Interceptor struct{}

// UnaryInterceptor is a grpc.UnaryServerInterceptor.
func (Interceptor) UnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	record(info.FullMethod, time.Since(start), err, true)
	return resp, err
}

// StreamInterceptor is a grpc.StreamServerInterceptor. Request duration is
// deliberately not observed for streams (see recordDuration=false below):
// StreamCommands (DeviceService's only streaming RPC) is a long-lived
// connection held open for a device's whole session — sometimes hours — not
// a single request/response. Timing it start-to-close against
// RequestDuration's bucket boundaries (prometheus.DefBuckets, capped at
// 10s) would put every observation in the +Inf overflow bucket, providing
// no usable signal while silently skewing the histogram's sample count.
// udal_requests_total still increments once the stream closes, by final
// status — a legitimate count of completed streaming sessions.
func (Interceptor) StreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()
	err := handler(srv, ss)
	record(info.FullMethod, time.Since(start), err, false)
	return err
}

func record(fullMethod string, dur time.Duration, err error, recordDuration bool) {
	op := operationName(fullMethod)
	Requests.WithLabelValues(op, status.Code(err).String()).Inc()
	if recordDuration {
		RequestDuration.WithLabelValues(op).Observe(dur.Seconds())
	}
}

// operationName extracts the short RPC name from gRPC's FullMethod
// ("/udal.v1.DeviceService/GetProperty" -> "GetProperty") — a stable, low-
// cardinality label value; the full method path (varying per service and
// stable per method) would work too, but the AC's own example
// ("udal_requests_total{operation,status}") reads as the short name.
func operationName(fullMethod string) string {
	if i := strings.LastIndexByte(fullMethod, '/'); i >= 0 {
		return fullMethod[i+1:]
	}
	return fullMethod
}
