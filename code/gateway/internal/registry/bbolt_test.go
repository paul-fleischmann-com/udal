package registry_test

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
)

func TestBboltRegisterGetRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := registry.NewBboltRegistry(path)
	if err != nil {
		t.Fatalf("NewBboltRegistry: %v", err)
	}
	defer r.Close()

	d := api.Device{
		Name:       "sensor-1",
		Capability: "temperature-sensor",
		Transport:  "mqtt",
		Labels:     map[string]string{"site": "hq"},
	}
	registered, err := r.Register(d)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if registered.ID == "" {
		t.Fatal("expected non-empty generated ID")
	}

	got, err := r.Get(registered.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != d.Name || got.Capability != d.Capability || got.Transport != d.Transport {
		t.Errorf("roundtrip mismatch: got %+v, want fields from %+v", got, d)
	}
	if got.Labels["site"] != "hq" {
		t.Errorf("Labels not preserved: got %+v", got.Labels)
	}
}

func TestBboltRegisterDuplicateID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := registry.NewBboltRegistry(path)
	if err != nil {
		t.Fatalf("NewBboltRegistry: %v", err)
	}
	defer r.Close()

	d := api.Device{ID: "fixed-id", Name: "cam", Capability: "ip-camera", Transport: "http"}
	if _, err := r.Register(d); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err = r.Register(d)
	if !errors.Is(err, registry.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestBboltSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")

	r1, err := registry.NewBboltRegistry(path)
	if err != nil {
		t.Fatalf("NewBboltRegistry: %v", err)
	}
	d, err := r1.Register(api.Device{Name: "pdu", Capability: "smart-pdu", Transport: "http"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r1.UpdateStatus(d.ID, api.DeviceStatusOnline, time.Now()); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := r1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate a gateway restart: reopen the same file.
	r2, err := registry.NewBboltRegistry(path)
	if err != nil {
		t.Fatalf("reopen NewBboltRegistry: %v", err)
	}
	defer r2.Close()

	got, err := r2.Get(d.ID)
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if got.Name != "pdu" || got.Status != api.DeviceStatusOnline {
		t.Errorf("entry did not survive restart intact: %+v", got)
	}
}

func TestBboltListFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := registry.NewBboltRegistry(path)
	if err != nil {
		t.Fatalf("NewBboltRegistry: %v", err)
	}
	defer r.Close()

	c1, err := r.Register(api.Device{Name: "c1", Capability: "ip-camera", Transport: "http", Labels: map[string]string{"site": "hq"}})
	if err != nil {
		t.Fatalf("Register c1: %v", err)
	}
	if _, err := r.Register(api.Device{Name: "s1", Capability: "temperature-sensor", Transport: "mqtt"}); err != nil {
		t.Fatalf("Register s1: %v", err)
	}
	if err := r.UpdateStatus(c1.ID, api.DeviceStatusOnline, time.Now()); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	all, err := r.List(registry.ListFilter{})
	if err != nil || len(all) != 2 {
		t.Errorf("List all: got %d devices, err %v, want 2", len(all), err)
	}

	tagged, err := r.List(registry.ListFilter{Tag: "site"})
	if err != nil || len(tagged) != 1 || tagged[0].ID != c1.ID {
		t.Errorf("List tag=site: got %v, err %v, want [%s]", tagged, err, c1.ID)
	}

	online := true
	onlineOnly, err := r.List(registry.ListFilter{Online: &online})
	if err != nil || len(onlineOnly) != 1 || onlineOnly[0].ID != c1.ID {
		t.Errorf("List online=true: got %v, err %v, want [%s]", onlineOnly, err, c1.ID)
	}
}

func TestBboltConcurrentAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := registry.NewBboltRegistry(path)
	if err != nil {
		t.Fatalf("NewBboltRegistry: %v", err)
	}
	defer r.Close()

	const n = 50
	var wg sync.WaitGroup
	ids := make([]string, n)

	for i := 0; i < n; i++ {
		d, err := r.Register(api.Device{Name: "d", Capability: "temperature-sensor", Transport: "mqtt"})
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		ids[i] = d.ID
	}

	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		id := ids[i]
		go func() {
			defer wg.Done()
			if _, err := r.Get(id); err != nil {
				t.Errorf("concurrent Get(%s): %v", id, err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := r.UpdateStatus(id, api.DeviceStatusOnline, time.Now()); err != nil {
				t.Errorf("concurrent UpdateStatus(%s): %v", id, err)
			}
		}()
	}
	wg.Wait()
}
