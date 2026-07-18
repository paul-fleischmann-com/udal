package service_test

import (
	"context"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	canadapter "github.com/paulefl/udal/code/gateway/internal/adapters/can"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc/codes"
)

// fakeCANAdapter implements service.CANAdapter for testing DeviceService's
// transport routing, without a real SocketCAN interface.
type fakeCANAdapter struct {
	readValue api.PropertyValue
	readErr   error
	writeErr  error

	watchedDevices []string
	readCalls      []string // "deviceID/path"
	writeCalls     []string // "deviceID/path"
}

func (f *fakeCANAdapter) ReadProperty(_ context.Context, d api.Device, path string) (api.PropertyValue, error) {
	f.readCalls = append(f.readCalls, d.ID+"/"+path)
	return f.readValue, f.readErr
}

func (f *fakeCANAdapter) WriteProperty(_ context.Context, d api.Device, path string, _ api.PropertyValue) error {
	f.writeCalls = append(f.writeCalls, d.ID+"/"+path)
	return f.writeErr
}

func (f *fakeCANAdapter) WatchDevice(_ context.Context, d api.Device) error {
	f.watchedDevices = append(f.watchedDevices, d.ID)
	return nil
}

func newSvcWithCAN(fake *fakeCANAdapter) *service.DeviceService {
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetCANAdapter(fake)
	return svc
}

func TestRegisterDevice_WatchesCANDevice(t *testing.T) {
	fake := &fakeCANAdapter{}
	svc := newSvcWithCAN(fake)

	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "engine-ecu", Capability: "engine-sensor", Transport: "can",
		Labels: map[string]string{canadapter.LabelMessage: "EngineData"},
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if len(fake.watchedDevices) != 1 || fake.watchedDevices[0] != reg.Device.Id {
		t.Errorf("watchedDevices = %v, want [%s]", fake.watchedDevices, reg.Device.Id)
	}
}

func TestRegisterDevice_NonCANDoesNotWatchCANAdapter(t *testing.T) {
	fake := &fakeCANAdapter{}
	svc := newSvcWithCAN(fake)

	if _, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	}); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if len(fake.watchedDevices) != 0 {
		t.Errorf("watchedDevices = %v, want none for a non-can device", fake.watchedDevices)
	}
}

func TestGetProperty_RoutesToCANAdapterForCANDevices(t *testing.T) {
	fake := &fakeCANAdapter{readValue: api.FloatValue(2748)}
	svc := newSvcWithCAN(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "engine-ecu", Capability: "engine-sensor", Transport: "can",
		Labels: map[string]string{canadapter.LabelMessage: "EngineData"},
	})

	resp, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "EngineSpeed",
	})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if resp.Value.GetFloatVal() != 2748 {
		t.Errorf("GetProperty = %v, want 2748", resp.Value.GetFloatVal())
	}
	want := reg.Device.Id + "/EngineSpeed"
	if len(fake.readCalls) != 1 || fake.readCalls[0] != want {
		t.Errorf("readCalls = %v, want [%s]", fake.readCalls, want)
	}
}

func TestGetProperty_NonCANDeviceIgnoresCANAdapter(t *testing.T) {
	fake := &fakeCANAdapter{readValue: api.FloatValue(999)}
	svc := newSvcWithCAN(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if _, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 5}},
	}); err != nil {
		t.Fatalf("SetProperty: %v", err)
	}

	resp, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if resp.Value.GetFloatVal() != 5 {
		t.Errorf("GetProperty = %v, want 5 (from PropertyStore, not the can fake)", resp.Value.GetFloatVal())
	}
	if len(fake.readCalls) != 0 {
		t.Errorf("readCalls = %v, want none for a non-can device", fake.readCalls)
	}
}

// TestSetProperty_CANDeviceRoutesToAdapter documents the scope decision
// distinguishing #25 from #24 (HTTP): F-11's AC explicitly lists
// WriteProperty, so SetProperty for a can-transport device must route to
// the adapter, not return Unimplemented like http-transport does.
func TestSetProperty_CANDeviceRoutesToAdapter(t *testing.T) {
	fake := &fakeCANAdapter{}
	svc := newSvcWithCAN(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "engine-ecu", Capability: "engine-sensor", Transport: "can",
		Labels: map[string]string{canadapter.LabelMessage: "EngineData"},
	})

	resp, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "EngineTemp",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: -10}},
	})
	if err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	if resp.NewValue.GetFloatVal() != -10 {
		t.Errorf("SetProperty response = %v, want -10", resp.NewValue.GetFloatVal())
	}
	want := reg.Device.Id + "/EngineTemp"
	if len(fake.writeCalls) != 1 || fake.writeCalls[0] != want {
		t.Errorf("writeCalls = %v, want [%s]", fake.writeCalls, want)
	}
}

func TestGetProperty_CANNoFrameYetMapsToNotFound(t *testing.T) {
	fake := &fakeCANAdapter{readErr: canadapter.ErrNoFrameYet}
	svc := newSvcWithCAN(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "engine-ecu", Capability: "engine-sensor", Transport: "can",
		Labels: map[string]string{canadapter.LabelMessage: "EngineData"},
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "EngineSpeed",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound for ErrNoFrameYet, got %v", err)
	}
}

func TestGetProperty_CANMissingLabelMapsToInvalidArgument(t *testing.T) {
	fake := &fakeCANAdapter{readErr: canadapter.ErrMissingLabel}
	svc := newSvcWithCAN(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "engine-ecu", Capability: "engine-sensor", Transport: "can",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "EngineSpeed",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for ErrMissingLabel, got %v", err)
	}
}

func TestGetProperty_CANNotOpenMapsToUnavailable(t *testing.T) {
	fake := &fakeCANAdapter{readErr: canadapter.ErrNotOpen}
	svc := newSvcWithCAN(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "engine-ecu", Capability: "engine-sensor", Transport: "can",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "EngineSpeed",
	})
	if grpcCode(err) != codes.Unavailable {
		t.Errorf("expected Unavailable for ErrNotOpen, got %v", err)
	}
}
