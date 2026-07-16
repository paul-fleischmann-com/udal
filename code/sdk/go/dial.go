package udal

import (
	"context"
	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// dial opens a gRPC connection to gatewayURL, encrypted with tlsConfig if
// given or plaintext otherwise.
func dial(gatewayURL string, tlsConfig *tls.Config) (*grpc.ClientConn, error) {
	var creds credentials.TransportCredentials
	if tlsConfig != nil {
		creds = credentials.NewTLS(tlsConfig)
	} else {
		creds = insecure.NewCredentials()
	}
	return grpc.NewClient(gatewayURL, grpc.WithTransportCredentials(creds))
}

// withAPIKey returns a context carrying apiKey as the X-API-Key metadata
// header, or ctx unchanged if apiKey is empty.
func withAPIKey(ctx context.Context, apiKey string) context.Context {
	if apiKey == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "x-api-key", apiKey)
}
