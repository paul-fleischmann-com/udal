package udal

import "crypto/tls"

// Config configures a device-side connection (see [NewDevice]).
type Config struct {
	// GatewayURL is the gateway's gRPC address, e.g. "localhost:50051" — no
	// scheme; TLSConfig controls whether the connection is encrypted.
	GatewayURL string
	// DeviceID, if set, registers (or re-registers, across restarts) with a
	// stable identity. Left empty, the gateway assigns one on first Run and
	// [Device.ID] reports it afterwards.
	DeviceID string
	// Name is required for registration.
	Name string
	// Capability is the capability schema reference, e.g. "temperature-sensor".
	Capability string
	// Transport is reported to the gateway at registration time. Devices
	// using this SDK connect directly over gRPC (no transport adapter in
	// between), so this is typically "grpc".
	Transport string
	// Labels are arbitrary key/value tags attached to the device record.
	Labels map[string]string
	// APIKey, if set, is sent as the X-API-Key header on every call.
	APIKey string
	// TLSConfig configures the gRPC connection's transport security. Nil
	// means an insecure (plaintext) connection — only for local development
	// against a gateway started with UDAL_DEV_INSECURE=true.
	TLSConfig *tls.Config
}

// ClientConfig configures an application-side connection (see [NewClient]).
type ClientConfig struct {
	// GatewayURL is the gateway's gRPC address, e.g. "localhost:50051".
	GatewayURL string
	// APIKey, if set, is sent as the X-API-Key header on every call.
	APIKey string
	// TLSConfig configures the gRPC connection's transport security. Nil
	// means an insecure (plaintext) connection.
	TLSConfig *tls.Config
}
