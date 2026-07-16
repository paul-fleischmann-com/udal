package auth_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/auth"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

func contextWithPeerCert(cn string) context.Context {
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: cn}}
	tlsInfo := credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}}
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: tlsInfo})
}

func TestIdentityFromContext_WithClientCert(t *testing.T) {
	ctx := contextWithPeerCert("sensor-01")
	id, ok := auth.IdentityFromContext(ctx)
	if !ok {
		t.Fatal("expected an identity to be found")
	}
	if id.Role != auth.RoleDevice || id.Subject != "sensor-01" || id.DeviceID != "sensor-01" {
		t.Errorf("unexpected identity: %+v", id)
	}
}

func TestIdentityFromContext_NoPeer(t *testing.T) {
	if _, ok := auth.IdentityFromContext(context.Background()); ok {
		t.Error("expected no identity without peer info")
	}
}

func TestIdentityFromContext_NonTLSPeer(t *testing.T) {
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: nil})
	if _, ok := auth.IdentityFromContext(ctx); ok {
		t.Error("expected no identity for a non-TLS peer")
	}
}

func TestIdentityFromContext_NoClientCert(t *testing.T) {
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{}})
	if _, ok := auth.IdentityFromContext(ctx); ok {
		t.Error("expected no identity when TLS connection presented no client cert")
	}
}
