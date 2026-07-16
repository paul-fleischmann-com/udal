package auth_test

import (
	"context"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/auth"
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
