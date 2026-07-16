//go:build integration

package mqtt

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// TestIntegration_ReadWriteSubscribe exercises the Adapter's v5 client
// against a real broker (issue #11's "Integration test against Mosquitto in
// CI (Docker)"). CI provides one via the go-integration job's mosquitto
// service (.github/workflows/ci.yml) and sets UDAL_TEST_MQTT_BROKER; run
// locally the same way against any MQTT broker, e.g.:
//
//	UDAL_TEST_MQTT_BROKER=tcp://127.0.0.1:1883 go test -tags integration ./internal/adapters/mqtt/...
//
// The "device" side is simulated with a second, independent connection
// (connectV3 — this incidentally also re-verifies genuine v3.1.1 wire
// compatibility) rather than shelling out to mosquitto_pub/sub, since the
// CI runner has no MQTT CLI tools, only the Docker broker itself.
func TestIntegration_ReadWriteSubscribe(t *testing.T) {
	brokerURL := os.Getenv("UDAL_TEST_MQTT_BROKER")
	if brokerURL == "" {
		t.Skip("UDAL_TEST_MQTT_BROKER not set")
	}
	ctx := context.Background()

	var mu sync.Mutex
	var updates []string
	a := New(brokerURL, func(deviceID, path string, v api.PropertyValue) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, deviceID+"/"+path)
	})
	if err := a.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer a.Disconnect(ctx)
	if err := a.WatchDevice(ctx, "dev-1"); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}

	// dev simulates the device side. The closure below only fires once a
	// message actually arrives, which can't happen before dev.Subscribe is
	// called further down (after dev is assigned), so the capture-by-
	// reference here is safe despite dev being nil while connectV3 runs.
	var dev transport
	respond := func(topic string, _ []byte) {
		switch {
		case strings.HasSuffix(topic, "/get"):
			_ = dev.Publish(ctx, "udal/dev-1/props/temperature", []byte(`{"float":21.5}`))
		case strings.HasSuffix(topic, "/set"):
			_ = dev.Publish(ctx, "udal/dev-1/props/led/set/ack", []byte(`{}`))
		}
	}
	connectedDev, err := connectV3(ctx, brokerURL, respond)
	if err != nil {
		t.Fatalf("connectV3 (device side): %v", err)
	}
	dev = connectedDev
	defer dev.Disconnect(ctx)
	if err := dev.Subscribe(ctx, "udal/dev-1/props/temperature/get"); err != nil {
		t.Fatalf("device subscribe get: %v", err)
	}
	if err := dev.Subscribe(ctx, "udal/dev-1/props/led/set"); err != nil {
		t.Fatalf("device subscribe set: %v", err)
	}

	val, err := a.ReadProperty(ctx, "dev-1", "temperature")
	if err != nil {
		t.Fatalf("ReadProperty: %v", err)
	}
	if val.FloatVal == nil || *val.FloatVal != 21.5 {
		t.Fatalf("ReadProperty = %+v, want float 21.5", val)
	}

	if err := a.WriteProperty(ctx, "dev-1", "led", api.BoolValue(true)); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}

	if err := dev.Publish(ctx, "udal/dev-1/props/humidity", []byte(`{"float":55}`)); err != nil {
		t.Fatalf("device publish humidity: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(updates)
		mu.Unlock()
		if n >= 2 { // temperature (from the ReadProperty reply) + humidity
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	got := append([]string(nil), updates...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("fan-out updates = %v, want at least [dev-1/temperature dev-1/humidity]", got)
	}
}
