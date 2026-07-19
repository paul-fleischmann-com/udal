package service_test

import (
	"context"
	"errors"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/adapter"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc/codes"
)

// fakeTransport implements adapter.Transport for testing DeviceService's
// custom-transport routing (issue #26), without a real third-party
// adapter.
type fakeTransport struct {
	name           string
	readValue      api.PropertyValue
	readErr        error
	writeErr       error
	watchedDevices []string

	readCalls  []string
	writeCalls []string
}

func (f *fakeTransport) Name() string { return f.name }

func (f *fakeTransport) ReadProperty(_ context.Context, d api.Device, path string) (api.PropertyValue, error) {
	f.readCalls = append(f.readCalls, d.ID+"/"+path)
	return f.readValue, f.readErr
}

func (f *fakeTransport) WriteProperty(_ context.Context, d api.Device, path string, _ api.PropertyValue) error {
	f.writeCalls = append(f.writeCalls, d.ID+"/"+path)
	return f.writeErr
}

func (f *fakeTransport) WatchDevice(_ context.Context, d api.Device) error {
	f.watchedDevices = append(f.watchedDevices, d.ID)
	return nil
}

func newSvcWithCustom(name string, fake *fakeTransport) *service.DeviceService {
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetCustomTransports(map[string]adapter.Transport{name: fake})
	return svc
}

func TestRegisterDevice_WatchesCustomTransportDevice(t *testing.T) {
	fake := &fakeTransport{name: "echo"}
	svc := newSvcWithCustom("echo", fake)

	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "echo",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if len(fake.watchedDevices) != 1 || fake.watchedDevices[0] != reg.Device.Id {
		t.Errorf("watchedDevices = %v, want [%s]", fake.watchedDevices, reg.Device.Id)
	}
}

func TestGetProperty_RoutesToCustomTransport(t *testing.T) {
	fake := &fakeTransport{name: "echo", readValue: api.FloatValue(7.5)}
	svc := newSvcWithCustom("echo", fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "echo",
	})

	resp, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if resp.Value.GetFloatVal() != 7.5 {
		t.Errorf("GetProperty = %v, want 7.5", resp.Value.GetFloatVal())
	}
	want := reg.Device.Id + "/temperature"
	if len(fake.readCalls) != 1 || fake.readCalls[0] != want {
		t.Errorf("readCalls = %v, want [%s]", fake.readCalls, want)
	}
}

func TestSetProperty_RoutesToCustomTransport(t *testing.T) {
	fake := &fakeTransport{name: "echo"}
	svc := newSvcWithCustom("echo", fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "echo",
	})

	setResp, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 12.5}},
	})
	if err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	if setResp.NewValue.GetFloatVal() != 12.5 {
		t.Errorf("SetProperty response FloatVal = %v, want 12.5", setResp.NewValue.GetFloatVal())
	}
	want := reg.Device.Id + "/temperature"
	if len(fake.writeCalls) != 1 || fake.writeCalls[0] != want {
		t.Errorf("writeCalls = %v, want [%s]", fake.writeCalls, want)
	}
}

func TestGetProperty_CustomTransportUnrecognizedError_MapsInternal(t *testing.T) {
	fake := &fakeTransport{name: "echo", readErr: errors.New("boom")}
	svc := newSvcWithCustom("echo", fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "echo",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.Internal {
		t.Errorf("expected Internal for an unrecognized custom-transport error, got %v", err)
	}
}

func TestSetProperty_CustomTransportWriteNotSupported_MapsUnimplemented(t *testing.T) {
	fake := &fakeTransport{name: "echo", writeErr: adapter.ErrWriteNotSupported}
	svc := newSvcWithCustom("echo", fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "echo",
	})

	_, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 1}},
	})
	if grpcCode(err) != codes.Unimplemented {
		t.Errorf("expected Unimplemented for ErrWriteNotSupported, got %v", err)
	}
}

func TestGetProperty_UnknownTransportIgnoresCustomAdaptersFallsBackToPropertyStore(t *testing.T) {
	fake := &fakeTransport{name: "echo", readValue: api.FloatValue(999)}
	svc := newSvcWithCustom("echo", fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if _, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 5}},
	}); err != nil {
		t.Fatalf("SetProperty (PropertyStore fallback, mqtt has no adapter configured here): %v", err)
	}

	resp, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if resp.Value.GetFloatVal() != 5 {
		t.Errorf("GetProperty = %v, want 5 (from PropertyStore, not the unrelated \"echo\" custom transport)", resp.Value.GetFloatVal())
	}
	if len(fake.readCalls) != 0 {
		t.Errorf("readCalls = %v, want none — device's transport is \"mqtt\", not \"echo\"", fake.readCalls)
	}
}

// TestRegisterDevice_CustomTransportCollidingWithBuiltinNameDoesNotDoubleWatch
// guards against a bug a code review caught before this shipped: a custom
// transport registered under a name that collides with a built-in one
// ("mqtt"/"http"/"can") used to fire WatchDevice on *both* the built-in
// adapter and the colliding custom one, double-subscribing the device.
// cmd/gateway/main.go now refuses to activate a custom adapter under a
// reserved name at startup — this test guards the same invariant directly
// at the DeviceService level, in case s.custom is ever populated with a
// colliding key some other way.
func TestRegisterDevice_CustomTransportCollidingWithBuiltinNameDoesNotDoubleWatch(t *testing.T) {
	mqttFake := &fakeMQTTAdapter{}
	customFake := &fakeTransport{name: "mqtt"}
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetMQTTAdapter(mqttFake)
	svc.SetCustomTransports(map[string]adapter.Transport{"mqtt": customFake})

	if _, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	}); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	if len(mqttFake.watchedDevices) != 1 {
		t.Errorf("built-in mqtt adapter watchedDevices = %v, want exactly 1", mqttFake.watchedDevices)
	}
	if len(customFake.watchedDevices) != 0 {
		t.Errorf("colliding custom transport watchedDevices = %v, want none — the built-in mqtt adapter must win, not both", customFake.watchedDevices)
	}
}

func TestGetProperty_CustomTransportErrNotFound_MapsNotFound(t *testing.T) {
	fake := &fakeTransport{name: "echo", readErr: adapter.ErrNotFound}
	svc := newSvcWithCustom("echo", fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "echo",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound for adapter.ErrNotFound, got %v", err)
	}
}

func TestSetProperty_CustomTransportErrInvalidArgument_MapsInvalidArgument(t *testing.T) {
	fake := &fakeTransport{name: "echo", writeErr: adapter.ErrInvalidArgument}
	svc := newSvcWithCustom("echo", fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "echo",
	})

	_, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 1}},
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for adapter.ErrInvalidArgument, got %v", err)
	}
}
