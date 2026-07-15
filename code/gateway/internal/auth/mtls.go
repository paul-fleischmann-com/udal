package auth

import (
	"context"
	"crypto/x509"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// IdentityFromPeerCert derives an Identity from a verified mTLS client
// certificate. Per F-17, the certificate's CN becomes the identity; this
// project treats mTLS clients as devices (mTLS is the device-facing auth
// method per req42.adoc stakeholder mapping), with the CN taken to be that
// device's ID by convention.
func IdentityFromPeerCert(cert *x509.Certificate) Identity {
	return Identity{Subject: cert.Subject.CommonName, Role: RoleDevice, DeviceID: cert.Subject.CommonName}
}

// IdentityFromContext returns the Identity derived from the peer's mTLS
// client certificate, if the connection presented one. By the time a gRPC
// handler runs, Go's crypto/tls has already validated the certificate chain
// against the server's configured ClientCAs during the handshake — this
// function only extracts the resulting identity, it does not itself verify
// anything.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return Identity{}, false
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return Identity{}, false
	}
	if len(tlsInfo.State.PeerCertificates) == 0 {
		return Identity{}, false
	}
	return IdentityFromPeerCert(tlsInfo.State.PeerCertificates[0]), true
}
