package heartbeat_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/heartbeat"
	"github.com/paulefl/udal/code/gateway/internal/registry"
)

// captureBroker records every PropertyUpdate published to it, safe for
// concurrent use, without needing a real Subscribe stream.
type captureBroker struct {
	*api.Broker
	mu   sync.Mutex
	got  []api.PropertyUpdate
	subs <-chan api.PropertyUpdate
}

func newCaptureBroker(t *testing.T, deviceID string) *captureBroker {
	t.Helper()
	b := api.NewBroker()
	ch, unsubscribe := b.Subscribe(deviceID)
	t.Cleanup(unsubscribe)
	cb := &captureBroker{Broker: b, subs: ch}
	go func() {
		for u := range ch {
			cb.mu.Lock()
			cb.got = append(cb.got, u)
			cb.mu.Unlock()
		}
	}()
	return cb
}

func (cb *captureBroker) events() []api.PropertyUpdate {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return append([]api.PropertyUpdate(nil), cb.got...)
}

func waitForEvents(t *testing.T, cb *captureBroker, n int) {
	t.Helper()
	deadline := time.After(time.Second)
	for len(cb.events()) < n {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d event(s), got %d", n, len(cb.events()))
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func registerDevice(t *testing.T, reg registry.Registry) string {
	t.Helper()
	d, err := reg.Register(api.Device{Name: "dev"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return d.ID
}

func TestMonitor_TouchTransitionsUnknownToOnlineAndEmits(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	deviceID := registerDevice(t, reg)
	broker := newCaptureBroker(t, deviceID)
	m := heartbeat.NewMonitor(reg, broker.Broker, time.Second, time.Minute)

	if err := m.Touch(deviceID); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	d, err := reg.Get(deviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Status != api.DeviceStatusOnline {
		t.Errorf("Status = %v, want Online", d.Status)
	}

	waitForEvents(t, broker, 1)
	ev := broker.events()[0]
	if ev.Status == nil || *ev.Status != api.DeviceStatusOnline {
		t.Errorf("event Status = %v, want Online", ev.Status)
	}
}

func TestMonitor_TouchWhileAlreadyOnlineDoesNotReEmit(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	deviceID := registerDevice(t, reg)
	broker := newCaptureBroker(t, deviceID)
	m := heartbeat.NewMonitor(reg, broker.Broker, time.Second, time.Minute)

	if err := m.Touch(deviceID); err != nil {
		t.Fatalf("Touch 1: %v", err)
	}
	waitForEvents(t, broker, 1)
	if err := m.Touch(deviceID); err != nil {
		t.Fatalf("Touch 2: %v", err)
	}

	// Give a possible (incorrect) second event time to arrive.
	time.Sleep(50 * time.Millisecond)
	if got := len(broker.events()); got != 1 {
		t.Errorf("events = %d, want exactly 1 (no re-emit while already online)", got)
	}
}

func TestMonitor_SweepTransitionsStaleOnlineDeviceToOffline(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	deviceID := registerDevice(t, reg)
	broker := newCaptureBroker(t, deviceID)
	m := heartbeat.NewMonitor(reg, broker.Broker, time.Second, time.Minute)

	lastSeen := time.Now().Add(-2 * time.Minute) // older than the 1-minute timeout
	if err := reg.UpdateStatus(deviceID, api.DeviceStatusOnline, lastSeen); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if err := m.Sweep(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	d, err := reg.Get(deviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Status != api.DeviceStatusOffline {
		t.Errorf("Status = %v, want Offline", d.Status)
	}
	if !d.LastSeen.Equal(lastSeen) {
		t.Errorf("LastSeen = %v, want unchanged %v (offline transition shouldn't bump it)", d.LastSeen, lastSeen)
	}

	waitForEvents(t, broker, 1)
	ev := broker.events()[0]
	if ev.Status == nil || *ev.Status != api.DeviceStatusOffline {
		t.Errorf("event Status = %v, want Offline", ev.Status)
	}
}

// TestMonitor_OnStatusChange covers issue #27's udal_devices_online gauge
// hook: WithOnStatusChange must fire alongside the broker publish on both
// Touch's online transition and Sweep's timeout-driven offline one.
func TestMonitor_OnStatusChange(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	deviceID := registerDevice(t, reg)
	broker := newCaptureBroker(t, deviceID)

	var mu sync.Mutex
	var calls []string
	m := heartbeat.NewMonitor(reg, broker.Broker, time.Second, time.Minute, heartbeat.WithOnStatusChange(func(id string, online bool) {
		mu.Lock()
		defer mu.Unlock()
		state := "offline"
		if online {
			state = "online"
		}
		calls = append(calls, id+"="+state)
	}))

	if err := m.Touch(deviceID); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	waitForEvents(t, broker, 1)

	lastSeen := time.Now().Add(-2 * time.Minute)
	if err := reg.UpdateStatus(deviceID, api.DeviceStatusOnline, lastSeen); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := m.Sweep(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	waitForEvents(t, broker, 2)

	mu.Lock()
	defer mu.Unlock()
	want := []string{deviceID + "=online", deviceID + "=offline"}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Errorf("onStatusChange calls = %v, want %v", calls, want)
	}
}

func TestMonitor_SweepIgnoresFreshOnlineDevice(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	deviceID := registerDevice(t, reg)
	broker := newCaptureBroker(t, deviceID)
	m := heartbeat.NewMonitor(reg, broker.Broker, time.Second, time.Minute)

	if err := reg.UpdateStatus(deviceID, api.DeviceStatusOnline, time.Now()); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := m.Sweep(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	d, err := reg.Get(deviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Status != api.DeviceStatusOnline {
		t.Errorf("Status = %v, want unchanged Online", d.Status)
	}
	if got := len(broker.events()); got != 0 {
		t.Errorf("events = %d, want 0", got)
	}
}

func TestMonitor_SweepIgnoresUnknownAndOfflineDevices(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	deviceID := registerDevice(t, reg) // status Unknown, LastSeen zero-value (very "stale")
	broker := newCaptureBroker(t, deviceID)
	m := heartbeat.NewMonitor(reg, broker.Broker, time.Second, time.Minute)

	if err := m.Sweep(); err != nil {
		t.Fatalf("Sweep (Unknown): %v", err)
	}
	d, err := reg.Get(deviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Status != api.DeviceStatusUnknown {
		t.Errorf("Status = %v, want unchanged Unknown (never touched -> no online-to-offline transition to report)", d.Status)
	}

	if err := reg.UpdateStatus(deviceID, api.DeviceStatusOffline, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := m.Sweep(); err != nil {
		t.Fatalf("Sweep (already Offline): %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if got := len(broker.events()); got != 0 {
		t.Errorf("events = %d, want 0 (no event for Unknown or already-Offline devices)", got)
	}
}

func TestMonitor_RunStopsOnContextCancel(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	deviceID := registerDevice(t, reg)
	broker := newCaptureBroker(t, deviceID)
	m := heartbeat.NewMonitor(reg, broker.Broker, 10*time.Millisecond, time.Millisecond)

	if err := reg.UpdateStatus(deviceID, api.DeviceStatusOnline, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	waitForEvents(t, broker, 1) // Run's ticker should have swept at least once

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestNewMonitor_ZeroValuesUseDefaults(t *testing.T) {
	m := heartbeat.NewMonitor(registry.NewMemoryRegistry(), api.NewBroker(), 0, 0)
	if m.Interval() != heartbeat.DefaultInterval {
		t.Errorf("Interval() = %v, want DefaultInterval %v", m.Interval(), heartbeat.DefaultInterval)
	}
}

func TestNewMonitor_NegativeIntervalUsesDefault(t *testing.T) {
	// time.ParseDuration("-5s") succeeds without error, so a malformed
	// gateway.yaml heartbeat_interval could reach here as a negative
	// value, not just zero. time.NewTicker panics on <= 0 -- Run must
	// never be handed one, or it crashes the whole gateway process from
	// its own goroutine.
	m := heartbeat.NewMonitor(registry.NewMemoryRegistry(), api.NewBroker(), -5*time.Second, -5*time.Second)
	if m.Interval() != heartbeat.DefaultInterval {
		t.Errorf("Interval() = %v, want DefaultInterval %v for a negative input", m.Interval(), heartbeat.DefaultInterval)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	m.Run(ctx) // must not panic
}
