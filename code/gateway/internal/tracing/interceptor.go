package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpcstatus "google.golang.org/grpc/status"
)

// Interceptor starts the root "api" span for every gRPC request (also
// covering REST, proxied through the same server) — req42.adoc F-24 AC:
// "Every gRPC request produces a trace with spans: api, auth, router,
// adapter". Register it first in the interceptor chain, before
// logging.Interceptor and auth.Authenticator's: both read the span this
// establishes from ctx (logging.Interceptor for the request log line's
// trace_id; auth.Authenticator to parent its own "auth" child span under
// it). "router" and "adapter" spans are created deeper in the call stack,
// by service.DeviceService, for the two RPCs (GetProperty/SetProperty)
// that actually dispatch to a transport adapter — see its doc comments for
// why the other RPCs (ListDevices, RegisterDevice, ...) don't get those two
// spans.
type Interceptor struct{}

// UnaryInterceptor is a grpc.UnaryServerInterceptor.
func (Interceptor) UnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, span := otel.Tracer(TracerName).Start(ctx, "api")
	defer span.End()
	resp, err := handler(ctx, req)
	recordResult(span, err)
	return resp, err
}

// StreamInterceptor is a grpc.StreamServerInterceptor.
func (Interceptor) StreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, span := otel.Tracer(TracerName).Start(ss.Context(), "api")
	defer span.End()
	err := handler(srv, &tracedStream{ServerStream: ss, ctx: ctx})
	recordResult(span, err)
	return err
}

func recordResult(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, grpcstatus.Convert(err).Message())
	}
}

// tracedStream overrides Context() so downstream handlers (and the auth/
// logging interceptors layered on top of this one) observe the
// span-carrying context rather than the original stream's — mirroring
// logging.Interceptor's and auth.authenticatedStream's same pattern.
type tracedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedStream) Context() context.Context { return s.ctx }
