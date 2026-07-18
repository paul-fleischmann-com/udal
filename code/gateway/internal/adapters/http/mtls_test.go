package httpadapter

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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// genCert generates an ephemeral, in-memory self-signed certificate — same
// technique as service.selfSignedCert (integration_test.go), extended with
// an ExtKeyUsage parameter and IsCA:true so a client cert can double as its
// own trust anchor when added directly to a server's ClientCAs pool.
func genCert(t *testing.T, cn string, extKeyUsage x509.ExtKeyUsage) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{extKeyUsage},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal EC key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("build key pair: %v", err)
	}
	return tlsCert, parsed
}

func mtlsTestServer(t *testing.T, clientCAs *x509.CertPool) *httptest.Server {
	t.Helper()
	serverCert, _ := genCert(t, "localhost", x509.ExtKeyUsageServerAuth)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"float":1}`))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}
	srv.StartTLS()
	return srv
}

// TestReadProperty_PresentsClientCertificateForMTLS exercises issue #24's
// AC "gateway presents client cert to device when configured": a device
// endpoint that requires and verifies a client certificate accepts the
// request when the adapter's *http.Client (via WithHTTPClient) carries one.
func TestReadProperty_PresentsClientCertificateForMTLS(t *testing.T) {
	clientCert, clientX509 := genCert(t, "udal-gateway", x509.ExtKeyUsageClientAuth)
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(clientX509)

	srv := mtlsTestServer(t, clientCAs)
	defer srv.Close()

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{clientCert},
				// The test server's cert isn't in any trust store; this
				// test only exercises client-cert presentation, not the
				// gateway verifying the device's server identity.
				InsecureSkipVerify: true, //nolint:gosec
			},
		},
	}
	a := New(nil, WithHTTPClient(httpClient))
	if _, err := a.ReadProperty(context.Background(), deviceWithEndpoint(srv.URL), "temperature"); err != nil {
		t.Fatalf("ReadProperty with client cert: %v", err)
	}
}

// TestReadProperty_MissingClientCertificateRejectedByMTLSServer is the
// control for the test above: without WithHTTPClient configuring a client
// certificate, the same mTLS-required server rejects the handshake.
func TestReadProperty_MissingClientCertificateRejectedByMTLSServer(t *testing.T) {
	_, clientX509 := genCert(t, "udal-gateway", x509.ExtKeyUsageClientAuth)
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(clientX509)

	srv := mtlsTestServer(t, clientCAs)
	defer srv.Close()

	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
	}
	a := New(nil, WithHTTPClient(httpClient))
	if _, err := a.ReadProperty(context.Background(), deviceWithEndpoint(srv.URL), "temperature"); err == nil {
		t.Fatal("expected an error when no client certificate is presented to an mTLS-required server")
	}
}
