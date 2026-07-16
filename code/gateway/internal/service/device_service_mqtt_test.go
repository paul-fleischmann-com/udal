package service_test

import (
	"context"
	"errors"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	mqttadapter "github.com/paulefl/udal/code/gateway/internal/adapters/mqtt"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc/codes"
)

// fakeMQTTAdapter implements service.MQTTAdapter for testing DeviceService's
// transport routing, without a real broker.
type fakeMQTTAdapter struct {
	readValue      api.PropertyValue
	readErr        error
	writeErr       error
	watchedDevices []string

	readCalls  []string // "deviceID/path"
	writeCalls []string
}

func (f *fakeMQTTAdapter) ReadProperty(_ context.Context, deviceID, path string) (api.PropertyValue, error) {
	f.readCalls = append(f.readCalls, deviceID+"/"+path)
	return f.readValue, f.readErr
}

func (f *fakeMQTTAdapter) WriteProperty(_ context.Context, deviceID, path string, _ api.PropertyValue) error {
	f.writeCalls = append(f.writeCalls, deviceID+"/"+path)
	return f.writeErr
}

func (f *fakeMQTTAdapter) WatchDevice(_ context.Context, deviceID string) error {
	f.watchedDevices = append(f.watchedDevices, deviceID)
	return nil
}

func newSvcWithMQTT(fake *fakeMQTTAdapter) *service.DeviceService {
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetMQTTAdapter(fake)
	return svc
}

func TestRegisterDevice_WatchesMQTTDevice(t *testing.T) {
	fake := &fakeMQTTAdapter{}
	svc := newSvcWithMQTT(fake)

	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if len(fake.watchedDevices) != 1 || fake.watchedDevices[0] != reg.Device.Id {
		t.Errorf("watchedDevices = %v, want [%s]", fake.watchedDevices, reg.Device.Id)
	}
}

func TestRegisterDevice_NonMQTTDoesNotWatch(t *testing.T) {
	fake := &fakeMQTTAdapter{}
	svc := newSvcWithMQTT(fake)

	if _, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	}); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if len(fake.watchedDevices) != 0 {
		t.Errorf("watchedDevices = %v, want none for a non-mqtt device", fake.watchedDevices)
	}
}

func TestGetProperty_RoutesToMQTTAdapterForMQTTDevices(t *testing.T) {
	fake := &fakeMQTTAdapter{readValue: api.FloatValue(21.5)}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})

	resp, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if resp.Value.GetFloatVal() != 21.5 {
		t.Errorf("GetProperty = %v, want 21.5", resp.Value.GetFloatVal())
	}
	want := reg.Device.Id + "/temperature"
	if len(fake.readCalls) != 1 || fake.readCalls[0] != want {
		t.Errorf("readCalls = %v, want [%s]", fake.readCalls, want)
	}
}

func TestGetProperty_NonMQTTDeviceIgnoresAdapter(t *testing.T) {
	fake := &fakeMQTTAdapter{readValue: api.FloatValue(999)}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
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
		t.Errorf("GetProperty = %v, want 5 (from PropertyStore, not the mqtt fake)", resp.Value.GetFloatVal())
	}
	if len(fake.readCalls) != 0 {
		t.Errorf("readCalls = %v, want none for a non-mqtt device", fake.readCalls)
	}
}

func TestSetProperty_RoutesToMQTTAdapterAndSkipsBrokerPublish(t *testing.T) {
	fake := &fakeMQTTAdapter{}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})

	resp, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "led",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_BoolVal{BoolVal: true}},
	})
	if err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	if !resp.NewValue.GetBoolVal() {
		t.Errorf("SetProperty response BoolVal = false, want true")
	}
	want := reg.Device.Id + "/led"
	if len(fake.writeCalls) != 1 || fake.writeCalls[0] != want {
		t.Errorf("writeCalls = %v, want [%s]", fake.writeCalls, want)
	}
}

func TestGetProperty_MQTTCircuitOpenMapsToUnavailable(t *testing.T) {
	fake := &fakeMQTTAdapter{readErr: mqttadapter.ErrCircuitOpen}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.Unavailable {
		t.Errorf("expected Unavailable for ErrCircuitOpen, got %v", err)
	}
}

func TestGetProperty_MQTTGenericErrorMapsToInternal(t *testing.T) {
	fake := &fakeMQTTAdapter{readErr: errors.New("some unexpected mqtt failure")}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.Internal {
		t.Errorf("expected Internal for a generic mqtt error, got %v", err)
	}
}

func TestSetProperty_MQTTWriteErrorPropagates(t *testing.T) {
	fake := &fakeMQTTAdapter{writeErr: errors.New("mqtt: write property dev-1/led: context deadline exceeded")}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})

	_, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "led",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_BoolVal{BoolVal: true}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
}
