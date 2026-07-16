package mqtt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/packets"
	"github.com/eclipse/paho.golang/paho"
)

// v5ConnectTimeout bounds how long connectV5 waits for the first CONNACK
// (success or failure) before giving up. It must be shorter than any
// caller-supplied ctx deadline is expected to be for Connect as a whole,
// since a v3.1.1 fallback attempt may still need to run afterwards.
const v5ConnectTimeout = 5 * time.Second

// v5Transport adapts autopaho.ConnectionManager to the transport interface.
type v5Transport struct {
	cm *autopaho.ConnectionManager
}

// connectV5 dials brokerURL as MQTT v5. If the broker rejects the CONNECT
// specifically because it doesn't support v5 (CONNACK reason code
// "Unsupported Protocol Version"), it stops retrying and returns
// errUnsupportedVersion so the caller can fall back to v3.1.1.
func connectV5(ctx context.Context, brokerURL string, onMessage func(topic string, payload []byte)) (transport, error) {
	u, err := url.Parse(brokerURL)
	if err != nil {
		return nil, fmt.Errorf("mqtt: parse broker URL: %w", err)
	}

	outcome := make(chan error, 1)
	report := func(err error) {
		select {
		case outcome <- err:
		default: // only the first attempt's outcome decides v5-vs-fallback
		}
	}

	cfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		ReconnectBackoff:              autopaho.NewExponentialBackoff(time.Second, 60*time.Second, time.Second, 2.0),
		ConnectTimeout:                v5ConnectTimeout,
		OnConnectionUp: func(*autopaho.ConnectionManager, *paho.Connack) {
			report(nil)
		},
		OnConnectError: func(err error) {
			report(err)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "udal-gateway-" + randHex(),
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					onMessage(pr.Packet.Topic, pr.Packet.Payload)
					return true, nil
				},
			},
		},
	}

	cm, err := autopaho.NewConnection(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("mqtt: v5 connect: %w", err)
	}

	connectCtx, cancel := context.WithTimeout(ctx, v5ConnectTimeout)
	defer cancel()
	select {
	case err := <-outcome:
		if err != nil {
			var cerr *autopaho.ConnackError
			if errors.As(err, &cerr) && cerr.ReasonCode == packets.ConnackUnsupportedProtocolVersion {
				_ = cm.Disconnect(context.Background())
				return nil, errUnsupportedVersion
			}
			_ = cm.Disconnect(context.Background())
			return nil, fmt.Errorf("mqtt: v5 connect: %w", err)
		}
	case <-connectCtx.Done():
		_ = cm.Disconnect(context.Background())
		return nil, fmt.Errorf("mqtt: v5 connect: %w", connectCtx.Err())
	}
	return &v5Transport{cm: cm}, nil
}

func (t *v5Transport) Publish(ctx context.Context, topic string, payload []byte) error {
	_, err := t.cm.Publish(ctx, &paho.Publish{Topic: topic, Payload: payload, QoS: 1})
	if err != nil {
		return fmt.Errorf("mqtt: v5 publish %s: %w", topic, err)
	}
	return nil
}

func (t *v5Transport) Subscribe(ctx context.Context, topic string) error {
	_, err := t.cm.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{{Topic: topic, QoS: 1}},
	})
	if err != nil {
		return fmt.Errorf("mqtt: v5 subscribe %s: %w", topic, err)
	}
	return nil
}

func (t *v5Transport) Disconnect(ctx context.Context) error {
	return t.cm.Disconnect(ctx)
}

func randHex() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
