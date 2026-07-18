package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// connectFlags are the connection/auth flags every "schema" subcommand
// accepts. Not shared with code/sdk/go's own dial/withAPIKey (unexported
// there, and that SDK is for application/device code, not admin tooling —
// see the plan doc's Design-Entscheidungen) — this is a small, independent
// copy scoped to what a CLI needs.
type connectFlags struct {
	gateway  string
	apiKey   string
	caCert   string
	insecure bool
}

func (f *connectFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.gateway, "gateway", envOr("UDAL_GATEWAY_ADDR", "localhost:50051"), "gateway gRPC address")
	fs.StringVar(&f.apiKey, "api-key", os.Getenv("UDAL_API_KEY"), "sent as the X-API-Key header")
	fs.StringVar(&f.caCert, "ca", os.Getenv("UDAL_TLS_CA"), "path to a CA certificate to verify the gateway's server certificate")
	fs.BoolVar(&f.insecure, "insecure", envOr("UDAL_DEV_INSECURE", "false") == "true", "connect without TLS (local development only)")
}

// dial opens a gRPC connection per f's flags.
func (f *connectFlags) dial() (*grpc.ClientConn, error) {
	if f.insecure {
		return grpc.NewClient(f.gateway, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	tlsConfig := &tls.Config{}
	if f.caCert != "" {
		pem, err := os.ReadFile(f.caCert)
		if err != nil {
			return nil, fmt.Errorf("read CA certificate %s: %w", f.caCert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse CA certificate %s: no valid certificates found", f.caCert)
		}
		tlsConfig.RootCAs = pool
	}
	return grpc.NewClient(f.gateway, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
}

// authContext returns ctx carrying f.apiKey as the X-API-Key metadata
// header, or ctx unchanged if apiKey is empty.
func (f *connectFlags) authContext(ctx context.Context) context.Context {
	if f.apiKey == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "x-api-key", f.apiKey)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// grpcMessage extracts a gRPC status's message verbatim — issue #23 AC1:
// "udal schema publish rejects invalid schemas with the same error the
// gateway API would return" means passing the server's message through
// unaltered, not reformatting it. Falls back to err.Error() for a
// non-status error (e.g. the connection itself failed).
func grpcMessage(err error) string {
	if s, ok := status.FromError(err); ok {
		return s.Message()
	}
	return err.Error()
}
