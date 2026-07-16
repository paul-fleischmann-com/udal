package mqtt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// defaultRequestTimeout is F-09/issue #11's "configurable timeout (default
// 5s)" for ReadProperty and WriteProperty. Not yet wired to gateway config
// (#41 YAML config isn't done); a constructor option overrides it per
// Adapter instance.
const defaultRequestTimeout = 5 * time.Second

// OnPropertyUpdate is called for every property value the adapter learns
// about — both replies to ReadProperty and unsolicited device publishes —
// so the gateway can fan it out via api.Broker (Subscribe RPC).
type OnPropertyUpdate func(deviceID, propertyPath string, v api.PropertyValue)

// Adapter is the MQTT transport adapter (req42.adoc F-09). It connects to
// one broker (v5, falling back to v3.1.1 — see Connect) and implements a
// request/response pattern over the topic convention documented in
// topics.go for ReadProperty/WriteProperty, plus fan-out of every property
// value the broker delivers.
type Adapter struct {
	brokerURL      string
	requestTimeout time.Duration
	onUpdate       OnPropertyUpdate
	log            *slog.Logger

	mu         sync.Mutex
	tr         transport
	subscribed map[string]struct{}
	waiters    map[string][]chan []byte

	cb *circuitBreaker

	// connectV5/connectV3 default to the real connectV5/connectV3 funcs;
	// overridable (unexported, same-package tests only) so the "fall back
	// to v3.1.1 on a v5-unsupported broker" decision in Connect can be unit
	// tested with a fake — no real v3.1.1-only broker is available to
	// trigger this path against actual infra (see plan doc).
	connectV5 connectFunc
	connectV3 connectFunc
}

// Option configures an Adapter constructed by New.
type Option func(*Adapter)

// WithRequestTimeout overrides defaultRequestTimeout for ReadProperty and
// WriteProperty.
func WithRequestTimeout(d time.Duration) Option {
	return func(a *Adapter) { a.requestTimeout = d }
}

// WithLogger overrides the Adapter's logger (default: slog.Default()).
func WithLogger(log *slog.Logger) Option {
	return func(a *Adapter) { a.log = log }
}

// New returns an Adapter for brokerURL (e.g. "mqtt://localhost:1883").
// onUpdate is called for every property value the adapter observes; it must
// not block. Call Connect before using the adapter.
func New(brokerURL string, onUpdate OnPropertyUpdate, opts ...Option) *Adapter {
	a := &Adapter{
		brokerURL:      brokerURL,
		requestTimeout: defaultRequestTimeout,
		onUpdate:       onUpdate,
		log:            slog.Default(),
		subscribed:     make(map[string]struct{}),
		waiters:        make(map[string][]chan []byte),
		cb:             newCircuitBreaker(),
		connectV5:      connectV5,
		connectV3:      connectV3,
	}
	return a
}

// Connect dials the broker, negotiating MQTT v5 first. Reconnection with
// exponential backoff (1s-60s) is handled internally by whichever transport
// connects (autopaho for v5, paho.mqtt.golang's AutoReconnect for v3.1.1)
// for the remaining lifetime of the connection. If the broker rejects v5
// specifically over protocol version, Connect falls back to v3.1.1
// automatically (issue #11: "MQTT v5 and v3.1.1 supported (auto-negotiate)").
func (a *Adapter) Connect(ctx context.Context) error {
	tr, err := a.connectV5(ctx, a.brokerURL, a.dispatch)
	if errors.Is(err, errUnsupportedVersion) {
		a.log.Info("mqtt: broker rejected v5, falling back to v3.1.1", "broker", a.brokerURL)
		tr, err = a.connectV3(ctx, a.brokerURL, a.dispatch)
	}
	if err != nil {
		return fmt.Errorf("mqtt: connect %s: %w", a.brokerURL, err)
	}
	a.mu.Lock()
	a.tr = tr
	a.mu.Unlock()
	return nil
}

// Disconnect closes the broker connection.
func (a *Adapter) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	tr := a.tr
	a.mu.Unlock()
	if tr == nil {
		return nil
	}
	return tr.Disconnect(ctx)
}

// WatchDevice subscribes to every property publish for deviceID
// (udal/{deviceId}/props/#), so OnPropertyUpdate fires for values the
// gateway never explicitly requested — the "Subscribe: fan-out to Router on
// incoming property events" acceptance criterion. Idempotent.
func (a *Adapter) WatchDevice(ctx context.Context, deviceID string) error {
	return a.subscribeOnce(ctx, topicPropsWildcard(deviceID))
}

// subscribeOnce issues an MQTT SUBSCRIBE for topic, unless it's already
// subscribed — either directly, or because an active wildcard subscription
// (see WatchDevice) already matches it. Skipping the redundant SUBSCRIBE
// matters: Mosquitto (and MQTT brokers generally) deliver a publish once
// per matching subscription on a client, so two overlapping subscriptions
// covering the same topic each hand the same message to dispatch, causing
// duplicate fan-out — confirmed against a real broker while testing.
func (a *Adapter) subscribeOnce(ctx context.Context, topic string) error {
	a.mu.Lock()
	if _, ok := a.subscribed[topic]; ok || a.coveredByWildcardLocked(topic) {
		a.mu.Unlock()
		return nil
	}
	tr := a.tr
	a.mu.Unlock()
	if tr == nil {
		return errors.New("mqtt: adapter not connected")
	}
	if err := tr.Subscribe(ctx, topic); err != nil {
		return err
	}
	a.mu.Lock()
	a.subscribed[topic] = struct{}{}
	a.mu.Unlock()
	return nil
}

// coveredByWildcardLocked reports whether topic is already matched by an
// active "props/#" wildcard subscription (see WatchDevice). Callers must
// hold a.mu.
func (a *Adapter) coveredByWildcardLocked(topic string) bool {
	deviceID, _, ok := parsePropsTopicOrSetAck(topic)
	if !ok {
		return false
	}
	_, subscribed := a.subscribed[topicPropsWildcard(deviceID)]
	return subscribed
}

// registerWaiter arranges for the next message(s) delivered on topic to be
// sent to the returned channel (buffered, size 1 — only the first delivery
// while waiting matters to ReadProperty/WriteProperty's single request).
// cancel must be called once the caller stops waiting, successfully or not.
func (a *Adapter) registerWaiter(topic string) (ch chan []byte, cancel func()) {
	ch = make(chan []byte, 1)
	a.mu.Lock()
	a.waiters[topic] = append(a.waiters[topic], ch)
	a.mu.Unlock()
	return ch, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		ws := a.waiters[topic]
		for i, w := range ws {
			if w == ch {
				a.waiters[topic] = append(ws[:i], ws[i+1:]...)
				break
			}
		}
		if len(a.waiters[topic]) == 0 {
			delete(a.waiters, topic)
		}
	}
}

// dispatch is the transport's onMessage callback: it delivers payload to
// every waiter registered on topic, and — if topic is a bare property
// value-publish topic (not a /get, /set or /set/ack request/ack topic) —
// invokes onUpdate for gateway fan-out.
func (a *Adapter) dispatch(topic string, payload []byte) {
	a.mu.Lock()
	waiters := a.waiters[topic]
	a.mu.Unlock()
	for _, w := range waiters {
		select {
		case w <- payload:
		default:
		}
	}

	deviceID, path, ok := parsePropsTopic(topic)
	if !ok {
		return
	}
	v, err := decodeValue(payload)
	if err != nil {
		a.log.Warn("mqtt: dropping malformed property payload", "topic", topic, "err", err)
		return
	}
	if a.onUpdate != nil {
		a.onUpdate(deviceID, path, v)
	}
}

// ReadProperty publishes to .../get and waits for the device's reply on the
// bare props/{path} topic, honoring ctx's deadline or the adapter's
// request timeout, whichever elapses first. Guarded by the circuit breaker
// (issue #11): returns ErrCircuitOpen without attempting the request after
// 5 consecutive ReadProperty/WriteProperty failures, for 30s.
func (a *Adapter) ReadProperty(ctx context.Context, deviceID, path string) (api.PropertyValue, error) {
	if err := a.cb.allow(); err != nil {
		return api.PropertyValue{}, err
	}
	v, err := a.readProperty(ctx, deviceID, path)
	if err != nil {
		a.cb.recordFailure()
	} else {
		a.cb.recordSuccess()
	}
	return v, err
}

func (a *Adapter) readProperty(ctx context.Context, deviceID, path string) (api.PropertyValue, error) {
	a.mu.Lock()
	tr := a.tr
	a.mu.Unlock()
	if tr == nil {
		return api.PropertyValue{}, errors.New("mqtt: adapter not connected")
	}

	replyTopic := topicProps(deviceID, path)
	if err := a.subscribeOnce(ctx, replyTopic); err != nil {
		return api.PropertyValue{}, fmt.Errorf("mqtt: subscribe %s: %w", replyTopic, err)
	}

	ctx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()

	ch, unregister := a.registerWaiter(replyTopic)
	defer unregister()

	if err := tr.Publish(ctx, topicGet(deviceID, path), nil); err != nil {
		return api.PropertyValue{}, err
	}

	select {
	case payload := <-ch:
		return decodeValue(payload)
	case <-ctx.Done():
		return api.PropertyValue{}, fmt.Errorf("mqtt: read property %s/%s: %w", deviceID, path, ctx.Err())
	}
}

// WriteProperty publishes to .../set and waits for the device's confirmation
// on .../set/ack, honoring ctx's deadline or the adapter's request timeout,
// whichever elapses first. Guarded by the circuit breaker; see ReadProperty.
func (a *Adapter) WriteProperty(ctx context.Context, deviceID, path string, v api.PropertyValue) error {
	if err := a.cb.allow(); err != nil {
		return err
	}
	err := a.writeProperty(ctx, deviceID, path, v)
	if err != nil {
		a.cb.recordFailure()
	} else {
		a.cb.recordSuccess()
	}
	return err
}

func (a *Adapter) writeProperty(ctx context.Context, deviceID, path string, v api.PropertyValue) error {
	a.mu.Lock()
	tr := a.tr
	a.mu.Unlock()
	if tr == nil {
		return errors.New("mqtt: adapter not connected")
	}

	ackTopic := topicSetAck(deviceID, path)
	if err := a.subscribeOnce(ctx, ackTopic); err != nil {
		return fmt.Errorf("mqtt: subscribe %s: %w", ackTopic, err)
	}

	ctx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()

	ch, unregister := a.registerWaiter(ackTopic)
	defer unregister()

	payload, err := encodeValue(v)
	if err != nil {
		return err
	}
	if err := tr.Publish(ctx, topicSet(deviceID, path), payload); err != nil {
		return err
	}

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqtt: write property %s/%s: %w", deviceID, path, ctx.Err())
	}
}
