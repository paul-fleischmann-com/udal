package auth

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const internalTestKID = "internal-test-key"

// TestJWTValidator_StaleCacheFallsBackThenUnavailable exercises F-18's "JWKS
// URL unreachable → cached keys used for up to 5 min; beyond that →
// UNAVAILABLE" — this needs to reach into JWTValidator's private cache
// timestamp, since waiting five real minutes isn't practical in a test.
func TestJWTValidator_StaleCacheFallsBackThenUnavailable(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
	body, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{"kty": "RSA", "kid": internalTestKID, "n": n, "e": e}},
	})
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))

	v, err := NewJWTValidator(srv.URL, "aud", "iss")
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "s", "aud": "aud", "iss": "iss", "exp": time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = internalTestKID
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	srv.Close() // JWKS now unreachable; cache still fresh (just fetched above)

	if _, err := v.Validate(signed); err != nil {
		t.Fatalf("expected cached keys to still validate right after JWKS goes down: %v", err)
	}

	// Simulate five-plus minutes having passed since the last successful fetch.
	v.mu.Lock()
	v.lastFetched = time.Now().Add(-jwksMaxStaleness - time.Second)
	v.mu.Unlock()

	_, err = v.Validate(signed)
	s, _ := status.FromError(err)
	if s.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable once past staleness window, got %v", err)
	}
}
