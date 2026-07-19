package service_test

import (
	"errors"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The three tests below cover issue #27's udal_adapter_errors_total{adapter}
// counter: DeviceService.GetProperty must record one at each of the three
// transport adapters' error call sites, using the same label names as the
// "component" logger scoping (mqtt_adapter/http_adapter/can_adapter) so the
// two are easy to cross-reference operationally.

func TestGetProperty_MQTTAdapterErrorIncrementsMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.AdapterErrors.WithLabelValues("mqtt_adapter"))

	fake := &fakeMQTTAdapter{readErr: errors.New("boom")}
	svc := newSvcWithMQTT(fake)
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if _, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	}); err == nil {
		t.Fatal("GetProperty: want error from fake adapter")
	}

	after := testutil.ToFloat64(metrics.AdapterErrors.WithLabelValues("mqtt_adapter"))
	if after != before+1 {
		t.Errorf("AdapterErrors{mqtt_adapter} = %v, want %v (before %v + 1)", after, before+1, before)
	}
}

func TestGetProperty_HTTPAdapterErrorIncrementsMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.AdapterErrors.WithLabelValues("http_adapter"))

	fake := &fakeHTTPAdapter{readErr: errors.New("boom")}
	svc := newSvcWithHTTP(fake)
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if _, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	}); err == nil {
		t.Fatal("GetProperty: want error from fake adapter")
	}

	after := testutil.ToFloat64(metrics.AdapterErrors.WithLabelValues("http_adapter"))
	if after != before+1 {
		t.Errorf("AdapterErrors{http_adapter} = %v, want %v (before %v + 1)", after, before+1, before)
	}
}

func TestGetProperty_CANAdapterErrorIncrementsMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.AdapterErrors.WithLabelValues("can_adapter"))

	fake := &fakeCANAdapter{readErr: errors.New("boom")}
	svc := newSvcWithCAN(fake)
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "engine-ecu", Capability: "engine-sensor", Transport: "can",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if _, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "EngineSpeed",
	}); err == nil {
		t.Fatal("GetProperty: want error from fake adapter")
	}

	after := testutil.ToFloat64(metrics.AdapterErrors.WithLabelValues("can_adapter"))
	if after != before+1 {
		t.Errorf("AdapterErrors{can_adapter} = %v, want %v (before %v + 1)", after, before+1, before)
	}
}
