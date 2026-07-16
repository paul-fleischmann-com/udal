package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"google.golang.org/grpc/codes"
)

const testKID = "test-key-1"

// jwksTestServer serves a JWKS document for the given key over HTTP, for
// tests to point a JWTValidator at.
func jwksTestServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
	body, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{"kty": "RSA", "kid": testKID, "n": n, "e": e}},
	})
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func signToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = testKID
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func mustGenerateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func TestJWTValidator_ValidToken(t *testing.T) {
	key := mustGenerateKey(t)
	srv := jwksTestServer(t, key)

	v, err := auth.NewJWTValidator(srv.URL, "udal-api", "https://issuer.example.com")
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}

	token := signToken(t, key, jwt.MapClaims{
		"sub":  "operator-1",
		"aud":  "udal-api",
		"iss":  "https://issuer.example.com",
		"exp":  time.Now().Add(time.Hour).Unix(),
		"role": "operator",
	})

	id, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if id.Subject != "operator-1" || id.Role != auth.RoleOperator {
		t.Errorf("unexpected identity: %+v", id)
	}
}

func TestJWTValidator_MissingRoleClaimDefaultsToReader(t *testing.T) {
	key := mustGenerateKey(t)
	srv := jwksTestServer(t, key)
	v, err := auth.NewJWTValidator(srv.URL, "udal-api", "https://issuer.example.com")
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}

	token := signToken(t, key, jwt.MapClaims{
		"sub": "someone",
		"aud": "udal-api",
		"iss": "https://issuer.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	id, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if id.Role != auth.RoleReader {
		t.Errorf("Role = %v, want RoleReader default", id.Role)
	}
}

func TestJWTValidator_Expired(t *testing.T) {
	key := mustGenerateKey(t)
	srv := jwksTestServer(t, key)
	v, err := auth.NewJWTValidator(srv.URL, "udal-api", "https://issuer.example.com")
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}

	token := signToken(t, key, jwt.MapClaims{
		"sub": "s", "aud": "udal-api", "iss": "https://issuer.example.com",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	_, err = v.Validate(token)
	if code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated for expired token, got %v", err)
	}
}

func TestJWTValidator_WrongAudience(t *testing.T) {
	key := mustGenerateKey(t)
	srv := jwksTestServer(t, key)
	v, err := auth.NewJWTValidator(srv.URL, "udal-api", "https://issuer.example.com")
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}

	token := signToken(t, key, jwt.MapClaims{
		"sub": "s", "aud": "someone-else", "iss": "https://issuer.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = v.Validate(token)
	if code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated for wrong audience, got %v", err)
	}
}

func TestJWTValidator_InvalidSignature(t *testing.T) {
	key := mustGenerateKey(t)
	wrongKey := mustGenerateKey(t)
	srv := jwksTestServer(t, key) // JWKS advertises `key`'s public half
	v, err := auth.NewJWTValidator(srv.URL, "udal-api", "https://issuer.example.com")
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}

	// Signed with a different key than the one in the JWKS.
	token := signToken(t, wrongKey, jwt.MapClaims{
		"sub": "s", "aud": "udal-api", "iss": "https://issuer.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = v.Validate(token)
	if code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated for invalid signature, got %v", err)
	}
}

func TestJWTValidator_JWKSUnreachableAtStartup(t *testing.T) {
	srv := jwksTestServer(t, mustGenerateKey(t))
	srv.Close() // never reachable

	_, err := auth.NewJWTValidator(srv.URL, "udal-api", "https://issuer.example.com")
	if err == nil {
		t.Fatal("expected NewJWTValidator to fail when the JWKS URL is unreachable")
	}
}
