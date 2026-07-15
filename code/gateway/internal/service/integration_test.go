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
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// selfSignedCert generates an ephemeral, in-memory self-signed TLS
// certificate for loopback testing — mirrors the TLS setup the real gateway
// binary does from files (see cmd/gateway/main.go), without touching disk.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal EC key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("build key pair: %v", err)
	}
	return cert
}

// TestGatewayTLS_RegisterReadSubscribe exercises the full stack the
// acceptance criteria of #8 describe: a real Go client dials the TLS-secured
// gRPC server, registers a device, reads a property, and receives a stream
// event via Subscribe.
func TestGatewayTLS_RegisterReadSubscribe(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	props := api.NewMemoryPropertyStore()
	broker := api.NewBroker()
	svc := service.New(reg, props, broker)

	cert := selfSignedCert(t)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})))
	udalv1.RegisterDeviceServiceServer(grpcServer, svc)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	// Same loopback-only reasoning as the REST gateway's internal dial in
	// main.go: this is our own freshly generated cert, dialed on localhost by
	// the process that just created it — no MITM surface to defend against.
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}))) //nolint:gosec
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := udalv1.NewDeviceServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	regResp, err := client.RegisterDevice(ctx, &udalv1.RegisterDeviceRequest{
		Name: "integration-sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := regResp.GetDevice().GetId()

	if _, err := client.SetProperty(ctx, &udalv1.SetPropertyRequest{
		DeviceId:     deviceID,
		PropertyPath: "temperature",
		Value:        &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 19.5}},
	}); err != nil {
		t.Fatalf("SetProperty: %v", err)
	}

	getResp, err := client.GetProperty(ctx, &udalv1.GetPropertyRequest{DeviceId: deviceID, PropertyPath: "temperature"})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if getResp.GetValue().GetFloatVal() != 19.5 {
		t.Errorf("GetProperty FloatVal = %v, want 19.5", getResp.GetValue().GetFloatVal())
	}

	subCtx, subCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer subCancel()
	stream, err := client.Subscribe(subCtx, &udalv1.SubscribeRequest{DeviceId: deviceID})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Give the server time to register the subscription before publishing.
	time.Sleep(100 * time.Millisecond)
	if _, err := client.SetProperty(ctx, &udalv1.SetPropertyRequest{
		DeviceId:     deviceID,
		PropertyPath: "temperature",
		Value:        &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 20.0}},
	}); err != nil {
		t.Fatalf("SetProperty (for stream event): %v", err)
	}

	start := time.Now()
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("Subscribe Recv: %v", err)
	}
	elapsed := time.Since(start)

	if event.GetPropertyPath() != "temperature" || event.GetValue().GetFloatVal() != 20.0 {
		t.Errorf("unexpected stream event: %+v", event)
	}
	t.Logf("stream event received after %s", elapsed)
}
