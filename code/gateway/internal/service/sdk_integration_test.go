//go:build integration

package service_test

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/auth"
	udal "github.com/paulefl/udal/code/sdk/go"
)

// TestSDK_DeviceAndClientEndToEnd exercises the #12 Go SDK acceptance
// criteria against a real gateway (device_service.go + the full AuthN/AuthZ
// stack from #9), using authTestServer (defined in auth_integration_test.go)
// so this doesn't need its own server setup:
//   - device registers, publishes a property, and handles a command via mTLS
//     (its client certificate's CN becomes its DeviceID identity)
//   - application reads that property and receives a live Subscribe event,
//     authenticated via API-Key instead — covering both auth methods the AC
//     asks for in one coherent scenario, rather than mixing both on a single
//     connection (see the plan doc: mTLS always resolves to RoleDevice, so a
//     caller presenting a cert can't also act as an API-Key-derived role).
func TestSDK_DeviceAndClientEndToEnd(t *testing.T) {
	_, ca, addr, _, apiKeys := authTestServer(t)

	if err := apiKeys.Put("admin-1", auth.RoleAdmin, "admin-key"); err != nil {
		t.Fatalf("provision admin key: %v", err)
	}

	const deviceID = "sdk-sensor-01"
	deviceCert := ca.issue(t, deviceID)

	device, err := udal.NewDevice(udal.Config{
		GatewayURL: addr,
		DeviceID:   deviceID,
		Name:       "sdk-sensor",
		Capability: "temperature-sensor",
		Transport:  "grpc",
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{deviceCert},
			RootCAs:      ca.pool,
			ServerName:   "localhost",
		},
	})
	if err != nil {
		t.Fatalf("NewDevice: %v", err)
	}
	defer device.Close()

	device.OnCommand("calibrate", func(params udal.Params) (any, error) {
		return map[string]any{"applied_offset": params.Float("offset")}, nil
	})

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runDone := make(chan error, 1)
	go func() { runDone <- device.Run(runCtx) }()

	// Give Run time to register and open its command stream before the
	// client starts driving it.
	time.Sleep(200 * time.Millisecond)

	if got := device.ID(); got != deviceID {
		t.Fatalf("Device.ID() = %q, want %q", got, deviceID)
	}
	if err := device.PublishProperty(context.Background(), "temperature", 21.5); err != nil {
		t.Fatalf("PublishProperty: %v", err)
	}

	client, err := udal.NewClient(udal.ClientConfig{
		GatewayURL: addr,
		APIKey:     "admin-key",
		TLSConfig:  &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-local loopback
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	appCtx, cancelApp := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelApp()

	val, err := client.GetProperty(appCtx, deviceID, "temperature")
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if val != 21.5 {
		t.Errorf("GetProperty = %v, want 21.5", val)
	}

	result, err := client.SendCommand(appCtx, deviceID, "calibrate", map[string]any{"offset": 1.5})
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if m, ok := result.(map[string]any); !ok || m["applied_offset"] != 1.5 {
		t.Errorf("SendCommand result = %v, want {applied_offset:1.5}", result)
	}

	subCtx, cancelSub := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSub()
	updates, errCh := client.Subscribe(subCtx, deviceID, "temperature")

	time.Sleep(500 * time.Millisecond) // let the Subscribe stream register before publishing
	if err := device.PublishProperty(context.Background(), "temperature", 22.0); err != nil {
		t.Fatalf("PublishProperty (for subscribe event): %v", err)
	}

	select {
	case update := <-updates:
		if update.Value != 22.0 {
			t.Errorf("Subscribe event value = %v, want 22.0", update.Value)
		}
	case err := <-errCh:
		t.Fatalf("Subscribe: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the subscribe event")
	}

	cancelRun()
	if err := <-runDone; err != nil {
		t.Errorf("Device.Run returned an error after cancel: %v", err)
	}
}
