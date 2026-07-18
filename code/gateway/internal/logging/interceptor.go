package logging

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Interceptor establishes a per-request trace ID (see GenerateTraceID) and
// logs exactly one JSON line per request — F-23's AC: "Request log line
// includes trace_id". Register it first in the interceptor chain (before
// auth.Authenticator's) so even a request that fails authentication gets a
// trace ID and a logged outcome, matching how grpc-gateway's REST
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
	ctx = WithTraceID(ctx, GenerateTraceID())
	start := time.Now()
	resp, err := handler(ctx, req)
	i.logRequest(ctx, info.FullMethod, time.Since(start), err)
	return resp, err
}

// StreamInterceptor is a grpc.StreamServerInterceptor.
func (i *Interceptor) StreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := WithTraceID(ss.Context(), GenerateTraceID())
	start := time.Now()
	err := handler(srv, &tracedStream{ServerStream: ss, ctx: ctx})
	i.logRequest(ctx, info.FullMethod, time.Since(start), err)
	return err
}

func (i *Interceptor) logRequest(ctx context.Context, method string, dur time.Duration, err error) {
	i.Log.InfoContext(ctx, "request",
		"method", method,
		"code", status.Code(err).String(),
		"duration_ms", dur.Milliseconds(),
	)
}

// tracedStream overrides Context() so downstream handlers (and the auth
// interceptor's identity-carrying context, layered on top of this one)
// observe the trace-ID-carrying context rather than the original stream's
// — mirroring auth.authenticatedStream's same pattern.
type tracedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedStream) Context() context.Context { return s.ctx }
