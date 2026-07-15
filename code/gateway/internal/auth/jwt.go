package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// jwksMaxStaleness is how long cached JWKS keys remain usable after the JWKS
// URL becomes unreachable, per F-18.
const jwksMaxStaleness = 5 * time.Minute

// jwk is the subset of RFC 7517 fields needed to reconstruct an RSA public
// key — the overwhelmingly common case for real-world OAuth2/OIDC providers.
// EC/OKP keys aren't supported; add them if a deployment ever needs one.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

// JWTValidator validates OAuth2 JWT Bearer tokens (F-18): signature against
// a JWKS endpoint, then aud/iss/exp claims. JWKS keys are cached and
// refreshed on every Validate call; if refreshing fails, the last-known-good
// keys remain usable for up to jwksMaxStaleness before Validate starts
// returning UNAVAILABLE instead of UNAUTHENTICATED.
type JWTValidator struct {
	jwksURL  string
	audience string
	issuer   string
	client   *http.Client

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastFetched time.Time
}

// NewJWTValidator does an initial JWKS fetch so a misconfigured JWKS URL
// fails at startup rather than on the first request.
func NewJWTValidator(jwksURL, audience, issuer string) (*JWTValidator, error) {
	v := &JWTValidator{
		jwksURL:  jwksURL,
		audience: audience,
		issuer:   issuer,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
	if err := v.refresh(); err != nil {
		return nil, fmt.Errorf("initial JWKS fetch from %q: %w", jwksURL, err)
	}
	return v, nil
}

func (v *JWTValidator) refresh() error {
	resp, err := v.client.Get(v.jwksURL) //nolint:gosec // URL is operator-configured, not user input
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch JWKS: unexpected status %d", resp.StatusCode)
	}

	var doc jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		key, err := rsaPublicKeyFromJWK(k)
		if err != nil {
			return fmt.Errorf("parse JWK %q: %w", k.Kid, err)
		}
		keys[k.Kid] = key
	}
	if len(keys) == 0 {
		return fmt.Errorf("JWKS document contained no usable RSA keys")
	}

	v.mu.Lock()
	v.keys = keys
	v.lastFetched = time.Now()
	v.mu.Unlock()
	return nil
}

func rsaPublicKeyFromJWK(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}

// Validate parses and validates rawToken. On success it returns the
// Identity derived from the `sub` claim and an optional `role` claim
// (defaulting to RoleReader — least privilege — if absent or not one of the
// known roles). Any validation failure returns codes.Unauthenticated, except
// a JWKS that's unreachable and past its staleness window, which returns
// codes.Unavailable per F-18.
func (v *JWTValidator) Validate(rawToken string) (Identity, error) {
	refreshErr := v.refresh() // best-effort; fall back to cache on failure

	v.mu.RLock()
	keys := v.keys
	staleFor := time.Since(v.lastFetched)
	v.mu.RUnlock()

	if refreshErr != nil && staleFor > jwksMaxStaleness {
		return Identity{}, status.Errorf(codes.Unavailable,
			"JWKS unreachable and cache stale (last refreshed %s ago): %v", staleFor.Round(time.Second), refreshErr)
	}

	token, err := jwt.Parse(rawToken, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown key id %q", kid)
		}
		return key, nil
	},
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}),
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return Identity{}, status.Errorf(codes.Unauthenticated, "invalid JWT: %v", err)
	}

	claims, _ := token.Claims.(jwt.MapClaims)
	sub, _ := claims["sub"].(string)

	role := RoleReader
	if r, ok := claims["role"].(string); ok {
		if parsed, valid := parseRole(r); valid {
			role = parsed
		}
	}
	return Identity{Subject: sub, Role: role}, nil
}

func parseRole(s string) (Role, bool) {
	switch Role(s) {
	case RoleAdmin, RoleOperator, RoleReader, RoleDevice:
		return Role(s), true
	default:
		return "", false
	}
}
