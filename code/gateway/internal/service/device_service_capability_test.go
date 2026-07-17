package service_test

import (
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/capability"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc/codes"
)

// fakeCapabilityRegistry implements service.CapabilityRegistry for testing
// DeviceService's F-14/F-15 wiring, without a real capability.Registry.
type fakeCapabilityRegistry struct {
	schemas map[string]capability.Schema // key: name@version
}

func newFakeCapabilityRegistry(schemas ...capability.Schema) *fakeCapabilityRegistry {
	m := make(map[string]capability.Schema, len(schemas))
	for _, s := range schemas {
		m[s.Name+"@"+s.Version] = s
	}
	return &fakeCapabilityRegistry{schemas: m}
}

func (f *fakeCapabilityRegistry) Get(name, version string) (capability.Schema, error) {
	s, ok := f.schemas[name+"@"+version]
	if !ok {
		return capability.Schema{}, capability.ErrNotFound
	}
	return s, nil
}

const testWidgetSchemaJSON = `{
	"udal": "1.0",
	"kind": "DeviceCapability",
	"metadata": {"name": "widget", "version": "1.0.0"},
	"properties": {
		"level": {"type": "int", "min": 0, "max": 100},
		"unit":  {"type": "enum", "values": ["celsius", "fahrenheit"]}
	}
}`

func mustParseTestSchema(raw string) capability.Schema {
	s, err := capability.Parse([]byte(raw))
	if err != nil {
		panic(err) // test fixture, must always parse
	}
	return s
}

func newSvcWithCapability(fake *fakeCapabilityRegistry) *service.DeviceService {
	svc := service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
	svc.SetCapabilityRegistry(fake)
	return svc
}

func TestRegisterDevice_UnknownCapabilitySchemaRejected(t *testing.T) {
	svc := newSvcWithCapability(newFakeCapabilityRegistry())
	_, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "widget@1.0.0", Transport: "http",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound for an unknown schema reference, got %v", err)
	}
}

func TestRegisterDevice_MalformedCapabilityRefRejected(t *testing.T) {
	svc := newSvcWithCapability(newFakeCapabilityRegistry())
	_, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "widget-no-version", Transport: "http",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound for a malformed (non name@version) reference, got %v", err)
	}
}

func TestRegisterDevice_KnownCapabilitySchemaAccepted(t *testing.T) {
	svc := newSvcWithCapability(newFakeCapabilityRegistry(mustParseTestSchema(testWidgetSchemaJSON)))
	_, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "widget@1.0.0", Transport: "http",
	})
	if err != nil {
		t.Errorf("RegisterDevice with a known schema reference: %v", err)
	}
}

func TestRegisterDevice_NoCapabilityRegistryConfiguredAcceptsAnything(t *testing.T) {
	svc := newSvc() // no SetCapabilityRegistry call
	_, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "anything-goes", Transport: "http",
	})
	if err != nil {
		t.Errorf("RegisterDevice without a configured CapabilityRegistry should be unaffected: %v", err)
	}
}

func TestSetProperty_ValidatesAgainstDeclaredSchema(t *testing.T) {
	svc := newSvcWithCapability(newFakeCapabilityRegistry(mustParseTestSchema(testWidgetSchemaJSON)))
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "widget@1.0.0", Transport: "http",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := reg.GetDevice().GetId()

	cases := []struct {
		name     string
		path     string
		value    *udalv1.PropertyValue
		wantCode codes.Code
	}{
		{
			"valid int within range", "level",
			&udalv1.PropertyValue{Value: &udalv1.PropertyValue_IntVal{IntVal: 50}},
			codes.OK,
		},
		{
			"string for int property", "level",
			&udalv1.PropertyValue{Value: &udalv1.PropertyValue_StringVal{StringVal: "fifty"}},
			codes.InvalidArgument,
		},
		{
			"int out of range", "level",
			&udalv1.PropertyValue{Value: &udalv1.PropertyValue_IntVal{IntVal: 999}},
			codes.InvalidArgument,
		},
		{
			"enum outside declared values", "unit",
			&udalv1.PropertyValue{Value: &udalv1.PropertyValue_StringVal{StringVal: "kelvin"}},
			codes.InvalidArgument,
		},
		{
			"valid enum value", "unit",
			&udalv1.PropertyValue{Value: &udalv1.PropertyValue_StringVal{StringVal: "celsius"}},
			codes.OK,
		},
		{
			"property not declared in schema", "nonexistent",
			&udalv1.PropertyValue{Value: &udalv1.PropertyValue_IntVal{IntVal: 1}},
			codes.NotFound,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
				DeviceId: deviceID, PropertyPath: c.path, Value: c.value,
			})
			if c.wantCode == codes.OK {
				if err != nil {
					t.Errorf("SetProperty(%s): unexpected error: %v", c.path, err)
				}
				return
			}
			if grpcCode(err) != c.wantCode {
				t.Errorf("SetProperty(%s) = %v, want code %v", c.path, err, c.wantCode)
			}
		})
	}
}

func TestSetProperty_NoCapabilityRegistryConfiguredSkipsValidation(t *testing.T) {
	svc := newSvc() // no SetCapabilityRegistry call
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "widget@1.0.0", Transport: "http",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	// Without a configured registry, an out-of-range/wrong-type value for a
	// property no schema was ever checked against must still be accepted --
	// behavior identical to before this feature existed.
	_, err = svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId: reg.GetDevice().GetId(), PropertyPath: "level",
		Value: &udalv1.PropertyValue{Value: &udalv1.PropertyValue_StringVal{StringVal: "not validated"}},
	})
	if err != nil {
		t.Errorf("SetProperty without a configured CapabilityRegistry should be unaffected: %v", err)
	}
}
