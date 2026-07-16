package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/heartbeat"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc/metadata"
)

// fakePresenceMonitor implements service.PresenceMonitor for testing
// DeviceService's presence wiring, without a real Monitor/registry.
type fakePresenceMonitor struct {
	interval time.Duration

	mu         sync.Mutex
	touchCalls []string
}

func (f *fakePresenceMonitor) Touch(deviceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touchCalls = append(f.touchCalls, deviceID)
	return nil
}
func (f *fakePresenceMonitor) Interval() time.Duration { return f.interval }

func (f *fakePresenceMonitor) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.touchCalls...)
}

func TestRegisterDevice_TouchesPresence(t *testing.T) {
	fake := &fakePresenceMonitor{}
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetPresenceMonitor(fake)

	resp, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "http",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	if calls := fake.calls(); len(calls) != 1 || calls[0] != resp.Device.Id {
		t.Errorf("touch calls = %v, want [%s]", calls, resp.Device.Id)
	}
}

func TestRegisterDevice_NoPresenceMonitorConfiguredIsFine(t *testing.T) {
	svc := newSvc() // no SetPresenceMonitor call
	if _, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "http",
	}); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
}

func TestRegisterDevice_ResponseReflectsTouchedStatus(t *testing.T) {
	// Uses the real Monitor (already unit tested standalone in
	// internal/heartbeat) rather than a fake, to confirm RegisterDevice's
	// response reflects the just-touched Online status rather than the
	// pre-touch Unknown snapshot Register itself returns.
	reg := registry.NewMemoryRegistry()
	broker := api.NewBroker()
	monitor := heartbeat.NewMonitor(reg, broker, time.Second, time.Minute)
	svc := service.New(reg, api.NewMemoryPropertyStore(), broker, api.NewCommandRouter())
	svc.SetPresenceMonitor(monitor)

	resp, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "http",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if resp.Device.Status != udalv1.DeviceStatus_DEVICE_STATUS_ONLINE {
		t.Errorf("Device.Status = %v, want DEVICE_STATUS_ONLINE", resp.Device.Status)
	}
}

func TestStreamCommands_TouchesPresenceImmediatelyOnConnect(t *testing.T) {
	fake := &fakePresenceMonitor{interval: time.Hour} // long enough that only the initial touch fires
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetPresenceMonitor(fake)

	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := reg.GetDevice().GetId()
	fake.mu.Lock()
	fake.touchCalls = nil // discard RegisterDevice's own touch, isolate StreamCommands'
	fake.mu.Unlock()

	ctx, cancel := context.WithCancel(adminCtx())
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-device-id", deviceID))
	stream := &fakeStreamCommandsStream{ctx: ctx, sendCh: make(chan *udalv1.Command, 1), recvCh: make(chan *udalv1.CommandResult, 1)}

	done := make(chan error, 1)
	go func() { done <- svc.StreamCommands(stream) }()

	deadline := time.After(time.Second)
	for len(fake.calls()) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the initial connect-time touch")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done

	if calls := fake.calls(); len(calls) != 1 || calls[0] != deviceID {
		t.Errorf("touch calls = %v, want exactly one [%s] (no ticks should have fired with a 1h interval)", calls, deviceID)
	}
}

func TestStreamCommands_TouchesPresencePeriodically(t *testing.T) {
	fake := &fakePresenceMonitor{interval: 10 * time.Millisecond}
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetPresenceMonitor(fake)

	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := reg.GetDevice().GetId()

	ctx, cancel := context.WithCancel(adminCtx())
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-device-id", deviceID))
	stream := &fakeStreamCommandsStream{ctx: ctx, sendCh: make(chan *udalv1.Command, 1), recvCh: make(chan *udalv1.CommandResult, 1)}

	done := make(chan error, 1)
	go func() { done <- svc.StreamCommands(stream) }()

	deadline := time.After(time.Second)
	for len(fake.calls()) < 3 { // initial touch + at least 2 ticks
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for repeated touches, got %v", fake.calls())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done
}

func TestStreamCommands_NoPresenceMonitorConfiguredIsFine(t *testing.T) {
	svc := newSvc() // no SetPresenceMonitor call
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := reg.GetDevice().GetId()

	ctx, cancel := context.WithCancel(adminCtx())
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-device-id", deviceID))
	stream := &fakeStreamCommandsStream{ctx: ctx, sendCh: make(chan *udalv1.Command, 1), recvCh: make(chan *udalv1.CommandResult, 1)}

	done := make(chan error, 1)
	go func() { done <- svc.StreamCommands(stream) }()
	time.Sleep(50 * time.Millisecond) // must not panic on a nil heartbeatTick select case
	cancel()
	if err := <-done; err != nil {
		t.Errorf("StreamCommands returned error after cancel: %v", err)
	}
}
