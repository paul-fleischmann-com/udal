package mqtt

import (
	"context"
	"fmt"
	"time"

	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

// v3ConnectTimeout bounds how long connectV3 waits for CONNECT to complete.
const v3ConnectTimeout = 5 * time.Second

// v3Transport adapts paho.mqtt.golang (v3.1.1-only, per its own package
// doc) to the transport interface. It's the fallback connectFunc used when
// connectV5 reports errUnsupportedVersion.
type v3Transport struct {
	cli mqttlib.Client
}

// connectV3 dials brokerURL as MQTT v3.1.1.
func connectV3(_ context.Context, brokerURL string, onMessage func(topic string, payload []byte)) (transport, error) {
	opts := mqttlib.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID("udal-gateway-" + randHex()).
		SetConnectTimeout(v3ConnectTimeout).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(60 * time.Second).
		SetDefaultPublishHandler(func(_ mqttlib.Client, m mqttlib.Message) {
			onMessage(m.Topic(), m.Payload())
		})

	cli := mqttlib.NewClient(opts)
	token := cli.Connect()
	if !token.WaitTimeout(v3ConnectTimeout) {
		cli.Disconnect(0)
		return nil, fmt.Errorf("mqtt: v3 connect %s: timed out", brokerURL)
	}
	if err := token.Error(); err != nil {
		cli.Disconnect(0)
		return nil, fmt.Errorf("mqtt: v3 connect %s: %w", brokerURL, err)
	}
	return &v3Transport{cli: cli}, nil
}

func (t *v3Transport) Publish(ctx context.Context, topic string, payload []byte) error {
	return waitToken(ctx, t.cli.Publish(topic, 1, false, payload))
}

func (t *v3Transport) Subscribe(ctx context.Context, topic string) error {
	// nil callback: route every message through the DefaultPublishHandler
	// set at connect time, same single-dispatch-path design as v5Transport.
	return waitToken(ctx, t.cli.Subscribe(topic, 1, nil))
}

func (t *v3Transport) Disconnect(_ context.Context) error {
	const quiesceMillis = 250
	t.cli.Disconnect(quiesceMillis)
	return nil
}

func waitToken(ctx context.Context, token mqttlib.Token) error {
	select {
	case <-token.Done():
		return token.Error()
	case <-ctx.Done():
		return ctx.Err()
	}
}
