// Package httpadapter implements the HTTP transport adapter (req42.adoc
// F-10, GitHub issue #24): devices exposed over HTTP are read via a
// synchronous GET per ReadProperty call, kept live for Subscribe via a
// background poller that periodically GETs a bulk property snapshot, and
// may additionally push updates ahead of their next scheduled poll via a
// webhook callback.
//
// Endpoint convention — a device's base URL comes from its
// Device.Labels[LabelEndpoint] (there is no separate per-device config
// field on Device; Labels is the existing extensibility mechanism, see
// api.Device):
//
//	GET  {endpoint}/properties/{path}   single property read (ReadProperty)
//	GET  {endpoint}/properties          bulk snapshot, {"path": {wire value}, ...}
//	                                    (used by the background poller)
//	POST {webhook_addr}/devices/{deviceId}/events   device pushes one property
//	                                    update ahead of its next poll:
//	                                    {"path": "...", "value": {wire value}}
//
// Unlike the MQTT adapter (#11), there's no persistent connection to
// maintain and therefore no circuit breaker — issue #24's acceptance
// criteria don't call for one, and a per-request timeout already bounds a
// misbehaving device's effect on a single call.
//
// WriteProperty isn't implemented: issue #24's acceptance criteria list
// only ReadProperty and Subscribe (webhook), not a write path, mirroring
// how #11 scoped SendCommand-over-MQTT out for the same reason (no AC
// requires it). SetProperty for http-transport devices therefore still
// falls through to DeviceService's in-memory PropertyStore, same as any
// unconfigured transport.
package httpadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// LabelEndpoint is the api.Device.Labels key holding a device's HTTP base
// URL (e.g. "https://10.0.0.5:8443"), required for ReadProperty/WatchDevice
// to do anything.
const LabelEndpoint = "http.endpoint"

// LabelPollInterval is the api.Device.Labels key holding an optional
// per-device override (a time.ParseDuration string, e.g. "10s") of the
// adapter's default poll interval (issue #24 AC: "Poll interval
// configurable per device (default 5 s)"). Invalid or absent falls back to
// the adapter-wide default set by WithPollInterval / defaultPollInterval.
const LabelPollInterval = "http.poll_interval"

const (
	defaultPollInterval   = 5 * time.Second
	defaultRequestTimeout = 5 * time.Second
)

// OnPropertyUpdate is called for every property value the adapter learns
// about — both direct ReadProperty replies aren't included here (only the
// poller and webhook path are, mirroring mqtt.OnPropertyUpdate's "both
// replies and unsolicited publishes" except HTTP has no unsolicited
// replies, only scheduled polls and webhook pushes) — so the gateway can
// fan it out via api.Broker (Subscribe RPC). Must not block.
type OnPropertyUpdate func(deviceID, propertyPath string, v api.PropertyValue)

// Adapter is the HTTP transport adapter. Construct with New, wire into
// DeviceService (service.SetHTTPAdapter), then call WatchDevice for every
// http-transport device — DeviceService's RegisterDevice does this
// automatically for newly-registered devices; main.go does it once at
// startup for devices registered in a previous run.
type Adapter struct {
	client         *http.Client
	onUpdate       OnPropertyUpdate
	pollInterval   time.Duration
	requestTimeout time.Duration
	log            *slog.Logger

	mu             sync.Mutex
	pollCancel     map[string]context.CancelFunc // deviceID -> stop its poll loop
	webhookDevices map[string]struct{}           // deviceID -> accepted by the webhook handler
}

// Option configures an Adapter constructed by New.
type Option func(*Adapter)

// WithHTTPClient overrides the *http.Client used for every outbound
// request (ReadProperty and the background poller). Use this to configure
// mTLS (issue #24 AC: "gateway presents client cert to device when
// configured") — build a client with a Transport whose TLSClientConfig
// carries the gateway's client certificate; cert loading itself stays in
// main.go, consistent with how the gateway's own server TLS cert is loaded
// there rather than inside a constructor. Default: &http.Client{} (no
// per-client timeout; every request already carries its own
// context.WithTimeout deadline, see WithRequestTimeout).
func WithHTTPClient(c *http.Client) Option { return func(a *Adapter) { a.client = c } }

// WithPollInterval overrides defaultPollInterval, the adapter-wide default
// used by WatchDevice for devices with no LabelPollInterval override.
func WithPollInterval(d time.Duration) Option { return func(a *Adapter) { a.pollInterval = d } }

// WithRequestTimeout overrides defaultRequestTimeout, applied per HTTP
// request (ReadProperty and each poll tick) via context.WithTimeout.
func WithRequestTimeout(d time.Duration) Option { return func(a *Adapter) { a.requestTimeout = d } }

// WithLogger overrides the Adapter's logger (default: slog.Default()).
func WithLogger(log *slog.Logger) Option { return func(a *Adapter) { a.log = log } }

// New returns an Adapter. onUpdate is called for every property value the
// poller or webhook observes; it must not block.
func New(onUpdate OnPropertyUpdate, opts ...Option) *Adapter {
	a := &Adapter{
		client:         &http.Client{},
		onUpdate:       onUpdate,
		pollInterval:   defaultPollInterval,
		requestTimeout: defaultRequestTimeout,
		log:            slog.Default(),
		pollCancel:     make(map[string]context.CancelFunc),
		webhookDevices: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// ReadProperty issues a single synchronous GET {endpoint}/properties/{path}
// against d's endpoint (d.Labels[LabelEndpoint]) and decodes the JSON
// response body into a typed api.PropertyValue (issue #24 AC). Independent
// of WatchDevice — a device never watched still supports on-demand
// ReadProperty, same as MQTT's ReadProperty doesn't require a prior
// WatchDevice.
func (a *Adapter) ReadProperty(ctx context.Context, d api.Device, path string) (api.PropertyValue, error) {
	endpoint := d.Labels[LabelEndpoint]
	if endpoint == "" {
		return api.PropertyValue{}, fmt.Errorf("http: device %q has no %s label", d.ID, LabelEndpoint)
	}
	ctx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()

	w, err := a.getValue(ctx, propertyURL(endpoint, path))
	if err != nil {
		return api.PropertyValue{}, fmt.Errorf("http: read property %s/%s: %w", d.ID, path, err)
	}
	return decodeValue(w), nil
}

// getValue performs the shared GET-and-decode-one-wireValue path for
// ReadProperty. A non-2xx response becomes a *StatusError so callers
// (device_service.go's httpStatusError) can map it to the right gRPC
// status.
func (a *Adapter) getValue(ctx context.Context, url string) (wireValue, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return wireValue{}, fmt.Errorf("build request: %w", err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return wireValue{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return wireValue{}, &StatusError{StatusCode: resp.StatusCode, Status: resp.Status}
	}
	var w wireValue
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		return wireValue{}, fmt.Errorf("decode property value: %w", err)
	}
	return w, nil
}

// WatchDevice starts d's background poll loop (see poller.go) at d's poll
// interval (LabelPollInterval override, else the adapter default) and
// registers d.ID with the webhook handler so incoming pushes for it are
// accepted (see webhook.go). Idempotent — a second call for an
// already-watched device is a no-op, mirroring mqtt.Adapter.WatchDevice's
// subscribeOnce behavior. ctx is not threaded into the poll loop: the loop
// must outlive the (typically request-scoped) ctx a caller like
// RegisterDevice passes in, so it's only consulted for immediate
// cancellation and then dropped in favor of an internally-owned context
// stopped by Close or a future per-device unwatch.
func (a *Adapter) WatchDevice(ctx context.Context, d api.Device) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	endpoint := d.Labels[LabelEndpoint]
	if endpoint == "" {
		return fmt.Errorf("http: device %q has no %s label", d.ID, LabelEndpoint)
	}

	a.mu.Lock()
	if _, ok := a.pollCancel[d.ID]; ok {
		a.mu.Unlock()
		return nil
	}
	interval := a.pollInterval
	if raw := d.Labels[LabelPollInterval]; raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	a.pollCancel[d.ID] = cancel
	a.webhookDevices[d.ID] = struct{}{}
	a.mu.Unlock()

	go a.pollLoop(pollCtx, d.ID, endpoint, interval)
	return nil
}

// Close stops every device's background poll loop. The webhook HTTP server
// itself is owned and shut down by main.go (Adapter only provides its
// Handler), same as the gRPC/REST servers own their own listeners.
func (a *Adapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, cancel := range a.pollCancel {
		cancel()
	}
	a.pollCancel = make(map[string]context.CancelFunc)
}

func propertyURL(endpoint, path string) string {
	return strings.TrimRight(endpoint, "/") + "/properties/" + path
}

func snapshotURL(endpoint string) string {
	return strings.TrimRight(endpoint, "/") + "/properties"
}
