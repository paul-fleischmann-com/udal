package registry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/paulefl/udal/gateway/internal/api"
	"github.com/paulefl/udal/gateway/internal/registry"
)

func newDevice(name, capability, transport string) api.Device {
	return api.Device{Name: name, Capability: capability, Transport: transport}
}

func TestRegister(t *testing.T) {
	r := registry.NewMemoryRegistry()
	d, err := r.Register(newDevice("sensor-1", "temperature-sensor", "mqtt"))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if d.ID == "" {
		t.Fatal("expected non-empty generated ID")
	}
	if d.Status != api.DeviceStatusUnknown {
		t.Errorf("initial status = %v, want Unknown", d.Status)
	}
}

func TestRegisterDuplicateID(t *testing.T) {
	r := registry.NewMemoryRegistry()
	d := api.Device{ID: "fixed-id", Name: "cam", Capability: "ip-camera", Transport: "http"}
	if _, err := r.Register(d); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := r.Register(d)
	if !errors.Is(err, registry.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestGet(t *testing.T) {
	r := registry.NewMemoryRegistry()
	got, err := r.Get("nonexistent")
	if !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got (%v, %v)", got, err)
	}

	d, _ := r.Register(newDevice("pdu", "smart-pdu", "http"))
	got, err = r.Get(d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "pdu" {
		t.Errorf("Name = %q, want %q", got.Name, "pdu")
	}
}

func TestList(t *testing.T) {
	r := registry.NewMemoryRegistry()
	if _, err := r.Register(newDevice("s1", "temperature-sensor", "mqtt")); err != nil {
		t.Fatalf("Register s1: %v", err)
	}
	if _, err := r.Register(newDevice("s2", "temperature-sensor", "mqtt")); err != nil {
		t.Fatalf("Register s2: %v", err)
	}
	if _, err := r.Register(newDevice("c1", "ip-camera", "http")); err != nil {
		t.Fatalf("Register c1: %v", err)
	}

	all, _ := r.List("", "")
	if len(all) != 3 {
		t.Errorf("List all: got %d, want 3", len(all))
	}

	mqtt, _ := r.List("temperature-sensor", "")
	if len(mqtt) != 2 {
		t.Errorf("List temperature-sensor: got %d, want 2", len(mqtt))
	}

	http, _ := r.List("", "http")
	if len(http) != 1 {
		t.Errorf("List http transport: got %d, want 1", len(http))
	}
}

func TestDelete(t *testing.T) {
	r := registry.NewMemoryRegistry()
	err := r.Delete("x")
	if !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	d, _ := r.Register(newDevice("x", "temperature-sensor", "mqtt"))
	if err := r.Delete(d.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = r.Get(d.ID)
	if !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("after Delete: expected ErrNotFound, got %v", err)
	}
}

func TestUpdateStatus(t *testing.T) {
	r := registry.NewMemoryRegistry()
	d, _ := r.Register(newDevice("s", "temperature-sensor", "mqtt"))

	ts := time.Now()
	if err := r.UpdateStatus(d.ID, api.DeviceStatusOnline, ts); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := r.Get(d.ID)
	if got.Status != api.DeviceStatusOnline {
		t.Errorf("Status = %v, want Online", got.Status)
	}
	if !got.LastSeen.Equal(ts) {
		t.Errorf("LastSeen = %v, want %v", got.LastSeen, ts)
	}

	err := r.UpdateStatus("missing", api.DeviceStatusOnline, ts)
	if !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
