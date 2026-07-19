package auth_test

import (
	"context"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/auth"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

func newAuthenticatorWithKey(t *testing.T, subject string, role auth.Role, rawKey string) *auth.Authenticator {
	t.Helper()
	store := newAPIKeyStore(t)
	if err := store.Put(subject, role, rawKey); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return &auth.Authenticator{APIKeys: store}
}

func echoHandler(ctx context.Context, _ any) (any, error) {
	id, _ := auth.Authenticated(ctx)
	return id, nil
}

func TestUnaryInterceptor_NoCredential(t *testing.T) {
	a := &auth.Authenticator{}
	_, err := a.UnaryInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, echoHandler)
	if code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}
}

func TestUnaryInterceptor_ValidAPIKey(t *testing.T) {
	a := newAuthenticatorWithKey(t, "svc-1", auth.RoleOperator, "the-key")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "the-key"))

	resp, err := a.UnaryInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, echoHandler)
	if err != nil {
		t.Fatalf("UnaryInterceptor: %v", err)
	}
	id := resp.(auth.Identity)
	if id.Subject != "svc-1" || id.Role != auth.RoleOperator {
		t.Errorf("unexpected identity: %+v", id)
	}
}

func TestUnaryInterceptor_InvalidAPIKey(t *testing.T) {
	a := newAuthenticatorWithKey(t, "svc-1", auth.RoleOperator, "the-key")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "wrong-key"))

	_, err := a.UnaryInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, echoHandler)
	if code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}
}

func TestUnaryInterceptor_MTLSTakesPriorityOverAPIKey(t *testing.T) {
	a := newAuthenticatorWithKey(t, "svc-1", auth.RoleOperator, "the-key")
	ctx := metadata.NewIncomingContext(contextWithPeerCert("sensor-01"), metadata.Pairs("x-api-key", "the-key"))

	resp, err := a.UnaryInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, echoHandler)
	if err != nil {
		t.Fatalf("UnaryInterceptor: %v", err)
	}
	id := resp.(auth.Identity)
	if id.Role != auth.RoleDevice || id.Subject != "sensor-01" {
		t.Errorf("expected mTLS identity to win, got %+v", id)
	}
}

// newTestTracerProvider registers a TracerProvider that exports
// synchronously into an in-memory recorder, mirroring
// internal/tracing/interceptor_test.go's helper — lets these tests assert
// on the "auth" span (req42.adoc F-24, issue #29) that authenticateTraced
// creates around every authenticate call.
func newTestTracerProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
	})
	return exp
}

func TestUnaryInterceptor_ValidAPIKey_RecordsAuthSpan(t *testing.T) {
	exp := newTestTracerProvider(t)
	a := newAuthenticatorWithKey(t, "svc-1", auth.RoleOperator, "the-key")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "the-key"))

	if _, err := a.UnaryInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, echoHandler); err != nil {
		t.Fatalf("UnaryInterceptor: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Name != "auth" {
		t.Fatalf("spans = %+v, want exactly one named \"auth\"", spans)
	}
	if spans[0].Status.Code.String() == "Error" {
		t.Errorf("span status = Error, want Unset/Ok for a successful authentication")
	}
}

func TestUnaryInterceptor_InvalidAPIKey_RecordsErrorOnAuthSpan(t *testing.T) {
	exp := newTestTracerProvider(t)
	a := newAuthenticatorWithKey(t, "svc-1", auth.RoleOperator, "the-key")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "wrong-key"))

	if _, err := a.UnaryInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, echoHandler); err == nil {
		t.Fatal("UnaryInterceptor: want error for invalid API key")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Name != "auth" {
		t.Fatalf("spans = %+v, want exactly one named \"auth\"", spans)
	}
	if spans[0].Status.Code.String() != "Error" {
		t.Errorf("span status = %v, want Error for a failed authentication", spans[0].Status.Code)
	}
}
