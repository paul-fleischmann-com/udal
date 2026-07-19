package adapter_test

import (
	"context"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/adapter"
	"github.com/paulefl/udal/code/gateway/internal/api"
)

// fakeTransport is a minimal adapter.Transport for registry tests.
type fakeTransport struct{ name string }

func (f *fakeTransport) Name() string { return f.name }
func (f *fakeTransport) ReadProperty(context.Context, api.Device, string) (api.PropertyValue, error) {
	return api.PropertyValue{}, nil
}
func (f *fakeTransport) WriteProperty(context.Context, api.Device, string, api.PropertyValue) error {
	return nil
}
func (f *fakeTransport) WatchDevice(context.Context, api.Device) error { return nil }

func TestRegisterAndLookup(t *testing.T) {
	tr := &fakeTransport{name: "registry-test-transport"}
	adapter.Register(tr.name, tr)

	got, ok := adapter.Lookup(tr.name)
	if !ok {
		t.Fatalf("Lookup(%q) = not found, want the transport just registered", tr.name)
	}
	if got != adapter.Transport(tr) {
		t.Errorf("Lookup(%q) returned a different Transport than was registered", tr.name)
	}
}

func TestLookup_UnknownNameNotFound(t *testing.T) {
	if _, ok := adapter.Lookup("no-such-transport-registered-anywhere"); ok {
		t.Error("Lookup for an unregistered name = found, want not found")
	}
}

func TestRegister_DuplicateNamePanics(t *testing.T) {
	adapter.Register("registry-test-duplicate", &fakeTransport{name: "registry-test-duplicate"})

	defer func() {
		if recover() == nil {
			t.Error("Register with an already-used name did not panic")
		}
	}()
	adapter.Register("registry-test-duplicate", &fakeTransport{name: "registry-test-duplicate"})
}
