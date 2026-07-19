package service_test

import (
	"context"
	"errors"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newTestTracerProvider registers a TracerProvider that exports
// synchronously into an in-memory recorder, mirroring
// internal/tracing/interceptor_test.go's helper — lets these tests assert
// on the "router"/"adapter" spans (req42.adoc F-24, issue #29) that
// GetProperty/SetProperty create around transport-adapter dispatch.
func newTestTracerProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
	})
	return exp
}

func spanNamed(spans tracetest.SpanStubs, name string) (tracetest.SpanStub, bool) {
	for _, s := range spans {
		if s.Name == name {
			return s, true
		}
	}
	return tracetest.SpanStub{}, false
}

func TestGetProperty_MQTTRoute_RecordsRouterAndAdapterSpans(t *testing.T) {
	exp := newTestTracerProvider(t)
	fake := &fakeMQTTAdapter{readValue: api.FloatValue(21.5)}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})

	if _, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	}); err != nil {
		t.Fatalf("GetProperty: %v", err)
	}

	spans := exp.GetSpans()
	router, ok := spanNamed(spans, "router")
	if !ok {
		t.Fatalf("spans = %+v, want a \"router\" span", spans)
	}
	adapter, ok := spanNamed(spans, "adapter")
	if !ok {
		t.Fatalf("spans = %+v, want an \"adapter\" span", spans)
	}
	if adapter.Parent.SpanID() != router.SpanContext.SpanID() {
		t.Errorf("adapter span's parent = %v, want router span %v (adapter must nest under router)", adapter.Parent.SpanID(), router.SpanContext.SpanID())
	}
}

func TestGetProperty_MQTTAdapterError_RecordsErrorOnBothSpans(t *testing.T) {
	exp := newTestTracerProvider(t)
	fake := &fakeMQTTAdapter{readErr: errors.New("boom")}
	svc := newSvcWithMQTT(fake)
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})

	if _, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	}); err == nil {
		t.Fatal("GetProperty: want error")
	}

	spans := exp.GetSpans()
	router, ok := spanNamed(spans, "router")
	if !ok || router.Status.Code.String() != "Error" {
		t.Errorf("router span status = %+v, want Error", router.Status)
	}
	adapter, ok := spanNamed(spans, "adapter")
	if !ok || adapter.Status.Code.String() != "Error" {
		t.Errorf("adapter span status = %+v, want Error", adapter.Status)
	}
}

func TestGetProperty_PropertyStoreFallback_RecordsRouterSpanOnlyNoAdapterSpan(t *testing.T) {
	exp := newTestTracerProvider(t)
	svc := newSvc()
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if _, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 1}},
	}); err != nil {
		t.Fatalf("SetProperty (seed via PropertyStore fallback, no mqtt adapter configured): %v", err)
	}
	exp.Reset()

	if _, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "temperature",
	}); err != nil {
		t.Fatalf("GetProperty: %v", err)
	}

	spans := exp.GetSpans()
	if _, ok := spanNamed(spans, "router"); !ok {
		t.Errorf("spans = %+v, want a \"router\" span even for the PropertyStore fallback route", spans)
	}
	if _, ok := spanNamed(spans, "adapter"); ok {
		t.Errorf("spans = %+v, want no \"adapter\" span for the PropertyStore fallback route (no transport adapter is dispatched to)", spans)
	}
}
