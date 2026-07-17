package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/config"
)

const sampleYAML = `
gateway:
  grpc_port: 50051
  http_port: 8080
  metrics_port: 9090
  tls:
    cert: /etc/udal/tls/server.crt
    key: /etc/udal/tls/server.key
    ca: /etc/udal/tls/ca.crt
  auth:
    api_key_header: X-API-Key
    jwks_url: https://auth.example.com/.well-known/jwks.json
  registry:
    path: /var/udal/registry.db
  adapters:
    mqtt:
      broker: tcp://mosquitto:1883
      client_id: udal-gateway
    http:
      poll_interval: 5s
      webhook_port: 8090
      mtls:
        cert: /etc/udal/tls/http-client.crt
        key: /etc/udal/tls/http-client.key
    can:
      interface: can0
  heartbeat_interval: 30s
  device_timeout: 90s
`

func writeFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	return path
}

func TestLoad_FullSample(t *testing.T) {
	cfg, err := config.Load(writeFile(t, sampleYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	g := cfg.Gateway
	if g.GRPCPort != 50051 || g.HTTPPort != 8080 || g.MetricsPort != 9090 {
		t.Errorf("ports = %+v, want 50051/8080/9090", g)
	}
	if g.TLS.Cert != "/etc/udal/tls/server.crt" || g.TLS.Key != "/etc/udal/tls/server.key" || g.TLS.CA != "/etc/udal/tls/ca.crt" {
		t.Errorf("tls = %+v", g.TLS)
	}
	if g.Auth.APIKeyHeader != "X-API-Key" || g.Auth.JWKSURL != "https://auth.example.com/.well-known/jwks.json" {
		t.Errorf("auth = %+v", g.Auth)
	}
	if g.Registry.Path != "/var/udal/registry.db" {
		t.Errorf("registry = %+v", g.Registry)
	}
	if g.Adapters.MQTT.Broker != "tcp://mosquitto:1883" || g.Adapters.MQTT.ClientID != "udal-gateway" {
		t.Errorf("adapters.mqtt = %+v", g.Adapters.MQTT)
	}
	if time.Duration(g.Adapters.HTTP.PollInterval) != 5*time.Second {
		t.Errorf("adapters.http.poll_interval = %v, want 5s", time.Duration(g.Adapters.HTTP.PollInterval))
	}
	if g.Adapters.HTTP.WebhookPort != 8090 {
		t.Errorf("adapters.http.webhook_port = %d, want 8090", g.Adapters.HTTP.WebhookPort)
	}
	if g.Adapters.HTTP.MTLS.Cert != "/etc/udal/tls/http-client.crt" || g.Adapters.HTTP.MTLS.Key != "/etc/udal/tls/http-client.key" {
		t.Errorf("adapters.http.mtls = %+v", g.Adapters.HTTP.MTLS)
	}
	if g.Adapters.CAN.Interface != "can0" {
		t.Errorf("adapters.can.interface = %q, want can0", g.Adapters.CAN.Interface)
	}
	if time.Duration(g.HeartbeatInterval) != 30*time.Second {
		t.Errorf("heartbeat_interval = %v, want 30s", time.Duration(g.HeartbeatInterval))
	}
	if time.Duration(g.DeviceTimeout) != 90*time.Second {
		t.Errorf("device_timeout = %v, want 90s", time.Duration(g.DeviceTimeout))
	}
}

func TestLoad_PartialFileLeavesRestZeroValue(t *testing.T) {
	cfg, err := config.Load(writeFile(t, "gateway:\n  grpc_port: 9999\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Gateway.GRPCPort != 9999 {
		t.Errorf("GRPCPort = %d, want 9999", cfg.Gateway.GRPCPort)
	}
	if cfg.Gateway.HTTPPort != 0 || cfg.Gateway.Registry.Path != "" {
		t.Errorf("expected unset fields to stay zero-value, got HTTPPort=%d Registry.Path=%q",
			cfg.Gateway.HTTPPort, cfg.Gateway.Registry.Path)
	}
}

func TestLoad_MissingFileReturnsZeroValueNoError(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load: %v, want nil error for a missing file (issue #41 AC)", err)
	}
	if cfg.Gateway.GRPCPort != 0 {
		t.Errorf("expected zero-value Config, got %+v", cfg)
	}
}

func TestLoad_MalformedFileIsAnError(t *testing.T) {
	_, err := config.Load(writeFile(t, "gateway: [this is not, a valid, mapping"))
	if err == nil {
		t.Fatal("expected an error for malformed YAML, got nil")
	}
}

func TestLoad_InvalidDurationIsAnError(t *testing.T) {
	_, err := config.Load(writeFile(t, "gateway:\n  heartbeat_interval: not-a-duration\n"))
	if err == nil {
		t.Fatal("expected an error for an invalid duration, got nil")
	}
}

func TestApplyEnv_OverridesEverySettableField(t *testing.T) {
	env := map[string]string{
		"UDAL_GRPC_PORT":          "1",
		"UDAL_HTTP_PORT":          "2",
		"UDAL_METRICS_PORT":       "3",
		"UDAL_TLS_CERT":           "cert.pem",
		"UDAL_TLS_KEY":            "key.pem",
		"UDAL_MTLS_CA_CERT":       "ca.pem",
		"UDAL_API_KEY_HEADER":     "X-Custom-Key",
		"UDAL_JWT_JWKS_URL":       "https://example.com/jwks.json",
		"UDAL_REGISTRY_PATH":      "/tmp/reg.db",
		"UDAL_MQTT_BROKER":        "tcp://broker:1883",
		"UDAL_MQTT_CLIENT_ID":     "custom-client",
		"UDAL_HTTP_POLL_INTERVAL": "10s",
		"UDAL_HTTP_WEBHOOK_PORT":  "8091",
		"UDAL_HTTP_MTLS_CERT":     "http-client-cert.pem",
		"UDAL_HTTP_MTLS_KEY":      "http-client-key.pem",
		"UDAL_CAN_INTERFACE":      "vcan0",
		"UDAL_HEARTBEAT_INTERVAL": "45s",
		"UDAL_DEVICE_TIMEOUT":     "120s",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}

	cfg := &config.Config{}
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	g := cfg.Gateway
	if g.GRPCPort != 1 || g.HTTPPort != 2 || g.MetricsPort != 3 {
		t.Errorf("ports = %+v", g)
	}
	if g.TLS.Cert != "cert.pem" || g.TLS.Key != "key.pem" || g.TLS.CA != "ca.pem" {
		t.Errorf("tls = %+v", g.TLS)
	}
	if g.Auth.APIKeyHeader != "X-Custom-Key" || g.Auth.JWKSURL != "https://example.com/jwks.json" {
		t.Errorf("auth = %+v", g.Auth)
	}
	if g.Registry.Path != "/tmp/reg.db" {
		t.Errorf("registry.path = %q", g.Registry.Path)
	}
	if g.Adapters.MQTT.Broker != "tcp://broker:1883" || g.Adapters.MQTT.ClientID != "custom-client" {
		t.Errorf("adapters.mqtt = %+v", g.Adapters.MQTT)
	}
	if time.Duration(g.Adapters.HTTP.PollInterval) != 10*time.Second {
		t.Errorf("adapters.http.poll_interval = %v", time.Duration(g.Adapters.HTTP.PollInterval))
	}
	if g.Adapters.HTTP.WebhookPort != 8091 {
		t.Errorf("adapters.http.webhook_port = %d", g.Adapters.HTTP.WebhookPort)
	}
	if g.Adapters.HTTP.MTLS.Cert != "http-client-cert.pem" || g.Adapters.HTTP.MTLS.Key != "http-client-key.pem" {
		t.Errorf("adapters.http.mtls = %+v", g.Adapters.HTTP.MTLS)
	}
	if g.Adapters.CAN.Interface != "vcan0" {
		t.Errorf("adapters.can.interface = %q", g.Adapters.CAN.Interface)
	}
	if time.Duration(g.HeartbeatInterval) != 45*time.Second || time.Duration(g.DeviceTimeout) != 120*time.Second {
		t.Errorf("heartbeat_interval/device_timeout = %v/%v", time.Duration(g.HeartbeatInterval), time.Duration(g.DeviceTimeout))
	}
}

func TestApplyEnv_LeavesFieldsUnsetWhenEnvVarAbsent(t *testing.T) {
	cfg := &config.Config{Gateway: config.Gateway{GRPCPort: 7777}}
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if cfg.Gateway.GRPCPort != 7777 {
		t.Errorf("GRPCPort = %d, want unchanged 7777 (no env var set)", cfg.Gateway.GRPCPort)
	}
}

func TestApplyEnv_InvalidIntIsAnError(t *testing.T) {
	t.Setenv("UDAL_GRPC_PORT", "not-a-number")
	cfg := &config.Config{}
	if err := cfg.ApplyEnv(); err == nil {
		t.Fatal("expected an error for a non-numeric UDAL_GRPC_PORT")
	}
}

func TestApplyEnv_InvalidDurationIsAnError(t *testing.T) {
	t.Setenv("UDAL_HEARTBEAT_INTERVAL", "not-a-duration")
	cfg := &config.Config{}
	if err := cfg.ApplyEnv(); err == nil {
		t.Fatal("expected an error for a non-duration UDAL_HEARTBEAT_INTERVAL")
	}
}

func TestResolveString(t *testing.T) {
	cases := []struct {
		name                                     string
		existingEnv, configValue, fallback, want string
	}{
		{"existing env wins over everything", "from-env", "from-config", "default", "from-env"},
		{"config wins when no existing env", "", "from-config", "default", "from-config"},
		{"fallback when neither set", "", "", "default", "default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := config.ResolveString(c.existingEnv, c.configValue, c.fallback); got != c.want {
				t.Errorf("ResolveString(%q, %q, %q) = %q, want %q", c.existingEnv, c.configValue, c.fallback, got, c.want)
			}
		})
	}
}

func TestResolveAddr(t *testing.T) {
	cases := []struct {
		name                    string
		existingEnv             string
		configPort, defaultPort int
		want                    string
	}{
		{"existing env wins verbatim", ":9999", 1234, 50051, ":9999"},
		{"config port used when no existing env", "", 1234, 50051, ":1234"},
		{"default port used when neither set", "", 0, 50051, ":50051"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := config.ResolveAddr(c.existingEnv, c.configPort, c.defaultPort); got != c.want {
				t.Errorf("ResolveAddr(%q, %d, %d) = %q, want %q", c.existingEnv, c.configPort, c.defaultPort, got, c.want)
			}
		})
	}
}
