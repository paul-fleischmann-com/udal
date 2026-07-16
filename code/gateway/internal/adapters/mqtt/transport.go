package mqtt

import (
	"context"
	"errors"
)

// errUnsupportedVersion is returned by a connectFunc when the broker
// rejected the CONNECT specifically due to protocol version (MQTT v5
// CONNACK reason code 0x84), signaling the adapter should fall back to the
// next protocol version rather than treat this as a fatal connect error.
var errUnsupportedVersion = errors.New("mqtt: broker rejected protocol version")

// transport is the minimal set of MQTT operations the adapter needs,
// implemented separately for v5 (v5.go, via autopaho) and v3.1.1 (v3.go,
// via paho.mqtt.golang) since no single actively-maintained Go client
// library covers both protocol versions (see docs/features/plans).
type transport interface {
	Publish(ctx context.Context, topic string, payload []byte) error
	Subscribe(ctx context.Context, topic string) error
	Disconnect(ctx context.Context) error
}

// connectFunc dials brokerURL and returns a ready transport. Every incoming
// publish (on any topic this transport is subscribed to) is delivered to
// onMessage as it arrives, from an internal goroutine — onMessage must not
// block.
type connectFunc func(ctx context.Context, brokerURL string, onMessage func(topic string, payload []byte)) (transport, error)
