package logging

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Interceptor logs exactly one JSON line per request — F-23's AC: "Request
// log line includes trace_id". It doesn't establish the trace ID itself;
// tracing.Interceptor (issue #29) does that by starting a real OpenTelemetry
// span, and contextHandler (handler.go) reads it back out of ctx
// automatically for every log call, including this one. Register
// tracing.Interceptor first in the chain, this one second — before
// auth.Authenticator's, so even a request that fails authentication gets a
// trace ID and a logged outcome — matching how grpc-gateway's REST
// translation layer also funnels through this same gRPC server, so one
// interceptor covers both surfaces.
type Interceptor struct {
	// Log is the logger the request-summary line is written to via
	// InfoContext. Callers are expected to pass one already scoped with
	// .With("component", "gateway.api") (see cmd/gateway/main.go) —
	// Interceptor doesn't add its own "component" attribute, so it isn't
	// hardcoded to one value here and callers don't end up with two
	// conflicting "component" keys on the same line.
	Log *slog.Logger
}

// UnaryInterceptor is a grpc.UnaryServerInterceptor.
func (i *Interceptor) UnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	i.logRequest(ctx, info.FullMethod, time.Since(start), err)
	return resp, err
}

// StreamInterceptor is a grpc.StreamServerInterceptor.
func (i *Interceptor) StreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()
	err := handler(srv, ss)
	i.logRequest(ss.Context(), info.FullMethod, time.Since(start), err)
	return err
}

func (i *Interceptor) logRequest(ctx context.Context, method string, dur time.Duration, err error) {
	i.Log.InfoContext(ctx, "request",
		"method", method,
		"code", status.Code(err).String(),
		"duration_ms", dur.Milliseconds(),
	)
}
