package service_test

import (
	"context"
	"errors"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	httpadapter "github.com/paulefl/udal/code/gateway/internal/adapters/http"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc/codes"
)

// fakeHTTPAdapter implements service.HTTPAdapter for testing DeviceService's
// transport routing, without a real device HTTP endpoint.
type fakeHTTPAdapter struct {
	readValue api.PropertyValue
	readErr   error

	watchedDevices []string
	readCalls      []string // "deviceID/path"
}

func (f *fakeHTTPAdapter) ReadProperty(_ context.Context, d api.Device, path string) (api.PropertyValue, error) {
	f.readCalls = append(f.readCalls, d.ID+"/"+path)
	return f.readValue, f.readErr
}

func (f *fakeHTTPAdapter) WatchDevice(_ context.Context, d api.Device) error {
	f.watchedDevices = append(f.watchedDevices, d.ID)
	return nil
}

func newSvcWithHTTP(fake *fakeHTTPAdapter) *service.DeviceService {
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetHTTPAdapter(fake)
	return svc
}

func TestRegisterDevice_WatchesHTTPDevice(t *testing.T) {
	fake := &fakeHTTPAdapter{}
	svc := newSvcWithHTTP(fake)

	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if len(fake.watchedDevices) != 1 || fake.watchedDevices[0] != reg.Device.Id {
		t.Errorf("watchedDevices = %v, want [%s]", fake.watchedDevices, reg.Device.Id)
	}
}

func TestRegisterDevice_NonHTTPDoesNotWatchHTTPAdapter(t *testing.T) {
	fake := &fakeHTTPAdapter{}
	svc := newSvcWithHTTP(fake)

	if _, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	}); err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if len(fake.watchedDevices) != 0 {
		t.Errorf("watchedDevices = %v, want none for a non-http device", fake.watchedDevices)
	}
}

func TestGetProperty_RoutesToHTTPAdapterForHTTPDevices(t *testing.T) {
	fake := &fakeHTTPAdapter{readValue: api.FloatValue(21.5)}
	svc := newSvcWithHTTP(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
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

func TestGetProperty_NonHTTPDeviceIgnoresHTTPAdapter(t *testing.T) {
	fake := &fakeHTTPAdapter{readValue: api.FloatValue(999)}
	svc := newSvcWithHTTP(fake)
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
		t.Errorf("GetProperty = %v, want 5 (from PropertyStore, not the http fake)", resp.Value.GetFloatVal())
	}
	if len(fake.readCalls) != 0 {
		t.Errorf("readCalls = %v, want none for a non-http device", fake.readCalls)
	}
}

// TestSetProperty_HTTPDeviceIsUnimplemented documents the scope decision in
// package httpadapter's doc comment: issue #24's AC list has no
// WriteProperty. SetProperty must not silently fall through to the
// in-memory PropertyStore here — GetProperty for this device always polls
// the (configured) HTTPAdapter live, so a PropertyStore write would be
// invisible to every subsequent read. Explicit Unimplemented instead.
func TestSetProperty_HTTPDeviceIsUnimplemented(t *testing.T) {
	fake := &fakeHTTPAdapter{}
	svc := newSvcWithHTTP(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	})

	_, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "led",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_BoolVal{BoolVal: true}},
	})
	if grpcCode(err) != codes.Unimplemented {
		t.Errorf("expected Unimplemented for SetProperty on an http-transport device, got %v", err)
	}
}

func TestGetProperty_HTTPNotFoundMapsToNotFound(t *testing.T) {
	fake := &fakeHTTPAdapter{readErr: &httpadapter.StatusError{StatusCode: 404, Status: "404 Not Found"}}
	svc := newSvcWithHTTP(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound for HTTP 404, got %v", err)
	}
}

func TestGetProperty_HTTPServerErrorMapsToUnavailable(t *testing.T) {
	fake := &fakeHTTPAdapter{readErr: &httpadapter.StatusError{StatusCode: 503, Status: "503 Service Unavailable"}}
	svc := newSvcWithHTTP(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.Unavailable {
		t.Errorf("expected Unavailable for HTTP 503, got %v", err)
	}
}

func TestGetProperty_HTTPBadRequestMapsToInvalidArgument(t *testing.T) {
	fake := &fakeHTTPAdapter{readErr: &httpadapter.StatusError{StatusCode: 400, Status: "400 Bad Request"}}
	svc := newSvcWithHTTP(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for HTTP 400, got %v", err)
	}
}

func TestGetProperty_HTTPGenericErrorMapsToInternal(t *testing.T) {
	fake := &fakeHTTPAdapter{readErr: errors.New("some unexpected http failure")}
	svc := newSvcWithHTTP(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	})

	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.Internal {
		t.Errorf("expected Internal for a generic http error, got %v", err)
	}
}
