//go:build integration

package service_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"path/filepath"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"go.etcd.io/bbolt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// testCA holds a self-signed CA plus a helper to mint further certs signed
// by it, for exercising real mTLS handshakes against a real CA chain rather
// than a single self-signed leaf.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "UDAL Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

// issue mints a new certificate with the given CN, signed by the CA.
func (ca *testCA) issue(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key for %q: %v", cn, err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("issue cert for %q: %v", cn, err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.cert.Raw}, PrivateKey: key}
}

// authTestServer starts a real gRPC server with mTLS-optional TLS (both
// client-cert and API-key/JWT callers can connect) and the same AuthN/AuthZ
// wiring as main.go, backed by an in-memory device registry so tests can
// call UpdateACL directly. Returns a dial function (no client cert — for
// API-Key/JWT callers), the CA + server address (for mTLS callers via
// mtlsDial), the registry, and the API key store.
func authTestServer(t *testing.T) (dial func(t *testing.T) *grpc.ClientConn, ca *testCA, addr string, reg registry.Registry, apiKeys *auth.APIKeyStore) {
	t.Helper()
	ca = newTestCA(t)
	serverCert := ca.issue(t, "localhost")

	reg = registry.NewMemoryRegistry()
	svc := service.New(reg, api.NewMemoryPropertyStore(), api.NewBroker())

	keyDB, err := bbolt.Open(filepath.Join(t.TempDir(), "keys.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("open key db: %v", err)
	}
	t.Cleanup(func() { keyDB.Close() })
	apiKeys, err = auth.NewAPIKeyStore(keyDB)
	if err != nil {
		t.Fatalf("NewAPIKeyStore: %v", err)
	}
	authenticator := &auth.Authenticator{APIKeys: apiKeys}

	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientCAs:    ca.pool,
			ClientAuth:   tls.VerifyClientCertIfGiven, // mTLS-optional: also allow API-key callers
			MinVersion:   tls.VersionTLS12,
		})),
		grpc.ChainUnaryInterceptor(authenticator.UnaryInterceptor),
		grpc.ChainStreamInterceptor(authenticator.StreamInterceptor),
	)
	udalv1.RegisterDeviceServiceServer(grpcServer, svc)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { lis.Close() })
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.GracefulStop)

	dial = func(t *testing.T) *grpc.ClientConn {
		t.Helper()
		conn, err := grpc.NewClient(lis.Addr().String(),
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}))) //nolint:gosec // test-local loopback
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	return dial, ca, lis.Addr().String(), reg, apiKeys
}

func withAPIKey(ctx context.Context, key string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "x-api-key", key)
}

func mtlsDial(t *testing.T, addr string, ca *testCA, clientCert tls.Certificate) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      ca.pool,
		ServerName:   "localhost",
	})))
	if err != nil {
		t.Fatalf("mTLS dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestIntegration_RBACDenialThenACLOverride is F-19 + F-20 end-to-end: a
// reader is denied SetProperty by RBAC, then permitted once a per-device ACL
// allow entry is added for them — there's no management RPC for this yet
// (see plan doc), so the test writes the ACL directly via the registry, the
// same way an eventual admin endpoint would.
func TestIntegration_RBACDenialThenACLOverride(t *testing.T) {
	dial, _, _, reg, apiKeys := authTestServer(t)

	if err := apiKeys.Put("admin-1", auth.RoleAdmin, "admin-key"); err != nil {
		t.Fatalf("provision admin key: %v", err)
	}
	if err := apiKeys.Put("reader-1", auth.RoleReader, "reader-key"); err != nil {
		t.Fatalf("provision reader key: %v", err)
	}

	client := udalv1.NewDeviceServiceClient(dial(t))
	adminCtx := withAPIKey(context.Background(), "admin-key")

	regResp, err := client.RegisterDevice(adminCtx, &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := regResp.GetDevice().GetId()

	readerCtx := withAPIKey(context.Background(), "reader-key")
	_, err = client.SetProperty(readerCtx, &udalv1.SetPropertyRequest{
		DeviceId: deviceID, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 1}},
	})
	if grpcCode(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied before ACL override, got %v", err)
	}

	if err := reg.UpdateACL(deviceID, []api.ACLEntry{{Subject: "reader-1", Allow: true}}); err != nil {
		t.Fatalf("UpdateACL: %v", err)
	}

	_, err = client.SetProperty(readerCtx, &udalv1.SetPropertyRequest{
		DeviceId: deviceID, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 2}},
	})
	if err != nil {
		t.Fatalf("expected ACL allow to override RBAC deny, got %v", err)
	}
}

// TestIntegration_MTLSDeviceOwnVsForeign is F-17 end-to-end: a real mTLS
// client certificate becomes a RoleDevice identity (CN = DeviceID), which
// may access its own device but not another one.
//
// Setup (registering the two devices) deliberately uses the no-client-cert
// dial + an admin API key, not a client certificate: authTestServer's
// mTLS-optional server (like every AuthN path in this package) tries mTLS
// first, so a caller presenting *any* client cert becomes a RoleDevice
// identity regardless of which API key it also sends — mixing the two on
// one connection doesn't produce an "admin" identity. That's also why
// main.go's internal REST-gateway dial must stay cert-free even when mTLS is
// required (see the loopback exemption in main.go): presenting some cert
// just to satisfy a handshake would otherwise silently turn every REST call
// into a device-scoped one.
func TestIntegration_MTLSDeviceOwnVsForeign(t *testing.T) {
	dial, ca, addr, _, apiKeys := authTestServer(t)

	if err := apiKeys.Put("admin-1", auth.RoleAdmin, "admin-key"); err != nil {
		t.Fatalf("provision admin key: %v", err)
	}

	adminClient := udalv1.NewDeviceServiceClient(dial(t))
	adminCtx := withAPIKey(context.Background(), "admin-key")

	dev1, err := adminClient.RegisterDevice(adminCtx, &udalv1.RegisterDeviceRequest{
		Name: "dev1", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice dev1: %v", err)
	}
	dev2, err := adminClient.RegisterDevice(adminCtx, &udalv1.RegisterDeviceRequest{
		Name: "dev2", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice dev2: %v", err)
	}
	if _, err := adminClient.SetProperty(adminCtx, &udalv1.SetPropertyRequest{
		DeviceId: dev1.GetDevice().GetId(), PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 21}},
	}); err != nil {
		t.Fatalf("admin SetProperty dev1: %v", err)
	}

	// Issue dev1 a client cert whose CN equals its own device ID, and dial
	// with *only* that cert — no API key — so mTLS is the sole credential.
	dev1Cert := ca.issue(t, dev1.GetDevice().GetId())
	dev1Client := udalv1.NewDeviceServiceClient(mtlsDial(t, addr, ca, dev1Cert))

	if _, err := dev1Client.GetProperty(context.Background(), &udalv1.GetPropertyRequest{
		DeviceId: dev1.GetDevice().GetId(), PropertyPath: "temperature",
	}); err != nil {
		t.Errorf("device should read its own property, got %v", err)
	}

	_, err = dev1Client.GetProperty(context.Background(), &udalv1.GetPropertyRequest{
		DeviceId: dev2.GetDevice().GetId(), PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("device reading a foreign device's property: expected PermissionDenied, got %v", err)
	}
}
