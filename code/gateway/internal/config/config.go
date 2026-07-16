// Package config loads the gateway's optional YAML configuration file
// (req42.adoc §7.2, GitHub issue #41). Every field is overridable by its
// documented UDAL_* environment variable; a missing file is not an error —
// callers get a zero-value Config, which resolves to the gateway's existing
// env-var-only defaults unchanged (see ResolveString/ResolveInt).
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration parses YAML/env duration strings like "30s" — time.Duration has
// no UnmarshalYAML of its own, so yaml.v3 would otherwise reject a string
// value or silently treat it as a nanosecond count.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Config mirrors req42.adoc §7.2's gateway.yaml schema. Not every field is
// consumed by the gateway yet — see docs/features/plans/41-yaml-config.md
// for which ones and why (metrics_port, auth.api_key_header,
// adapters.mqtt.client_id, adapters.http/can, heartbeat_interval and
// device_timeout are parsed/overridable but not yet wired to behavior).
type Config struct {
	Gateway Gateway `yaml:"gateway"`
}

type Gateway struct {
	GRPCPort          int      `yaml:"grpc_port"`
	HTTPPort          int      `yaml:"http_port"`
	MetricsPort       int      `yaml:"metrics_port"`
	TLS               TLS      `yaml:"tls"`
	Auth              Auth     `yaml:"auth"`
	Registry          Registry `yaml:"registry"`
	Adapters          Adapters `yaml:"adapters"`
	HeartbeatInterval Duration `yaml:"heartbeat_interval"`
	DeviceTimeout     Duration `yaml:"device_timeout"`
}

type TLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
	CA   string `yaml:"ca"`
}

type Auth struct {
	APIKeyHeader string `yaml:"api_key_header"`
	JWKSURL      string `yaml:"jwks_url"`
}

type Registry struct {
	Path string `yaml:"path"`
}

type Adapters struct {
	MQTT MQTTAdapter `yaml:"mqtt"`
	HTTP HTTPAdapter `yaml:"http"`
	CAN  CANAdapter  `yaml:"can"`
}

type MQTTAdapter struct {
	Broker   string `yaml:"broker"`
	ClientID string `yaml:"client_id"`
}

type HTTPAdapter struct {
	PollInterval Duration `yaml:"poll_interval"`
}

type CANAdapter struct {
	Interface string `yaml:"interface"`
}

// Load reads and parses the YAML config file at path. A missing file is
// not an error (issue #41: "Missing config file → falls back to current
// env-var-only defaults") — it returns a zero-value Config, which resolves
// to the gateway's existing defaults via ResolveString/ResolveInt. A
// present-but-malformed file is a hard error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}
	return &cfg, nil
}

// ApplyEnv overrides every Config field from its documented UDAL_*
// environment variable, for whichever are actually set (issue #41: "Every
// YAML key overridable by its documented UDAL_* environment variable").
func (c *Config) ApplyEnv() error {
	overrideString := func(dst *string, key string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	overrideInt := func(dst *int, key string) error {
		v := os.Getenv(key)
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: invalid integer %q: %w", key, v, err)
		}
		*dst = n
		return nil
	}
	overrideDuration := func(dst *Duration, key string) error {
		v := os.Getenv(key)
		if v == "" {
			return nil
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s: invalid duration %q: %w", key, v, err)
		}
		*dst = Duration(d)
		return nil
	}

	if err := overrideInt(&c.Gateway.GRPCPort, "UDAL_GRPC_PORT"); err != nil {
		return err
	}
	if err := overrideInt(&c.Gateway.HTTPPort, "UDAL_HTTP_PORT"); err != nil {
		return err
	}
	if err := overrideInt(&c.Gateway.MetricsPort, "UDAL_METRICS_PORT"); err != nil {
		return err
	}
	overrideString(&c.Gateway.TLS.Cert, "UDAL_TLS_CERT")
	overrideString(&c.Gateway.TLS.Key, "UDAL_TLS_KEY")
	overrideString(&c.Gateway.TLS.CA, "UDAL_MTLS_CA_CERT")
	overrideString(&c.Gateway.Auth.APIKeyHeader, "UDAL_API_KEY_HEADER")
	overrideString(&c.Gateway.Auth.JWKSURL, "UDAL_JWT_JWKS_URL")
	overrideString(&c.Gateway.Registry.Path, "UDAL_REGISTRY_PATH")
	overrideString(&c.Gateway.Adapters.MQTT.Broker, "UDAL_MQTT_BROKER")
	overrideString(&c.Gateway.Adapters.MQTT.ClientID, "UDAL_MQTT_CLIENT_ID")
	if err := overrideDuration(&c.Gateway.Adapters.HTTP.PollInterval, "UDAL_HTTP_POLL_INTERVAL"); err != nil {
		return err
	}
	overrideString(&c.Gateway.Adapters.CAN.Interface, "UDAL_CAN_INTERFACE")
	if err := overrideDuration(&c.Gateway.HeartbeatInterval, "UDAL_HEARTBEAT_INTERVAL"); err != nil {
		return err
	}
	if err := overrideDuration(&c.Gateway.DeviceTimeout, "UDAL_DEVICE_TIMEOUT"); err != nil {
		return err
	}
	return nil
}

// ResolveString returns, in order: existingEnvValue if non-empty (the
// gateway's pre-#41 flat env var, e.g. UDAL_REGISTRY_PATH — always wins, so
// existing deployments are unaffected), else configValue if non-empty
// (loaded from gateway.yaml, already possibly overridden by its own
// documented env var via ApplyEnv), else fallback.
func ResolveString(existingEnvValue, configValue, fallback string) string {
	if existingEnvValue != "" {
		return existingEnvValue
	}
	if configValue != "" {
		return configValue
	}
	return fallback
}

// ResolveAddr resolves a "host:port" listen address the same way
// ResolveString resolves a plain value, except the config-file/default
// layer is expressed as a bare port number (matching gateway.yaml's
// grpc_port/http_port/metrics_port) rather than a full address string.
func ResolveAddr(existingEnvValue string, configPort int, defaultPort int) string {
	if existingEnvValue != "" {
		return existingEnvValue
	}
	port := configPort
	if port == 0 {
		port = defaultPort
	}
	return fmt.Sprintf(":%d", port)
}
