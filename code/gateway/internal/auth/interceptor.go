package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type identityCtxKey struct{}

// Authenticated retrieves the Identity resolved by the AuthN interceptor for
// the current request. RPC handlers use this to run Authorize with the
// caller's identity.
func Authenticated(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityCtxKey{}).(Identity)
	return id, ok
}

// ContextWithIdentity returns a context carrying id, retrievable via
// Authenticated. The interceptors below are the production path for this;
// tests that call RPC handlers directly (bypassing the interceptor chain)
// use this to set up an authenticated context.
func ContextWithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// Authenticator resolves the caller's Identity for each incoming request,
// trying mTLS (peer client certificate), then the X-API-Key header, then a
// JWT Bearer Authorization header — first match wins. APIKeys and/or JWT may
// be nil to disable that method entirely (e.g. no JWTValidator configured
// because no JWKS URL was given).
type Authenticator struct {
	APIKeys *APIKeyStore
	JWT     *JWTValidator
}

func (a *Authenticator) authenticate(ctx context.Context) (Identity, error) {
	if id, ok := IdentityFromContext(ctx); ok {
		return id, nil
	}

	md, _ := metadata.FromIncomingContext(ctx)

	if a.APIKeys != nil {
		if keys := md.Get("x-api-key"); len(keys) > 0 && keys[0] != "" {
			id, err := a.APIKeys.Authenticate(keys[0])
			if err != nil {
				return Identity{}, status.Error(codes.Unauthenticated, "invalid API key")
			}
			return id, nil
		}
	}

	if a.JWT != nil {
		if authHeaders := md.Get("authorization"); len(authHeaders) > 0 {
			if token, ok := strings.CutPrefix(authHeaders[0], "Bearer "); ok {
				return a.JWT.Validate(token)
			}
		}
	}

	return Identity{}, status.Error(codes.Unauthenticated, "no valid credential provided (mTLS client cert, X-API-Key header, or Authorization: Bearer JWT)")
}

// UnaryInterceptor authenticates the caller and stores the resulting
// Identity in the context passed to the handler (and, transitively, to
// Authorize calls made from within it).
func (a *Authenticator) UnaryInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	id, err := a.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return handler(ContextWithIdentity(ctx, id), req)
}

// StreamInterceptor is the streaming-RPC equivalent of UnaryInterceptor,
// needed for Subscribe.
func (a *Authenticator) StreamInterceptor(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	id, err := a.authenticate(ss.Context())
	if err != nil {
		return err
	}
	return handler(srv, &authenticatedStream{
		ServerStream: ss,
		ctx:          ContextWithIdentity(ss.Context(), id),
	})
}

// authenticatedStream overrides Context() so downstream handlers observe the
// identity-carrying context rather than the original stream's.
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context { return s.ctx }
