package httpadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

type update struct {
	deviceID, path string
	value          api.PropertyValue
}

// updateRecorder is a concurrency-safe OnPropertyUpdate sink for tests —
// the poll loop invokes it from its own goroutine.
type updateRecorder struct {
	mu      sync.Mutex
	updates []update
}

func (r *updateRecorder) onUpdate(deviceID, path string, v api.PropertyValue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, update{deviceID, path, v})
}

func (r *updateRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.updates)
}

func (r *updateRecorder) snapshot() []update {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]update, len(r.updates))
	copy(out, r.updates)
	return out
}

func waitForCount(t *testing.T, r *updateRecorder, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.count() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d update(s), got %d", n, r.count())
}

// snapshotServer serves a mutable bulk snapshot at GET /properties, letting
// tests change a device's simulated state mid-poll-loop.
type snapshotServer struct {
	mu   sync.Mutex
	body map[string]wireValue
}

func (s *snapshotServer) set(body map[string]wireValue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
}

func (s *snapshotServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/properties" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	body := s.body
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(body)
}

func TestWatchDevice_PollsAndFansOutOnlyChanges(t *testing.T) {
	ss := &snapshotServer{}
	ss.set(map[string]wireValue{"temperature": {Float: floatPtr(21.5)}})
	srv := httptest.NewServer(ss)
	defer srv.Close()

	rec := &updateRecorder{}
	a := New(rec.onUpdate, WithPollInterval(20*time.Millisecond))
	d := deviceWithEndpoint(srv.URL)
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}
	defer a.Close()

	waitForCount(t, rec, 1, time.Second)

	// A few more ticks with an unchanged snapshot must not produce more
	// updates — only a change should fan out.
	time.Sleep(80 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Errorf("update count after unchanged ticks = %d, want 1 (no duplicate fan-out)", got)
	}

	ss.set(map[string]wireValue{"temperature": {Float: floatPtr(22.0)}})
	waitForCount(t, rec, 2, time.Second)

	got := rec.snapshot()[1]
	if got.deviceID != d.ID || got.path != "temperature" || got.value.FloatVal == nil || *got.value.FloatVal != 22.0 {
		t.Errorf("second update = %+v, want temperature=22.0 for %s", got, d.ID)
	}
}

func TestWatchDevice_Idempotent(t *testing.T) {
	ss := &snapshotServer{}
	ss.set(map[string]wireValue{"temperature": {Float: floatPtr(21.5)}})
	srv := httptest.NewServer(ss)
	defer srv.Close()

	rec := &updateRecorder{}
	a := New(rec.onUpdate, WithPollInterval(20*time.Millisecond))
	d := deviceWithEndpoint(srv.URL)
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("first WatchDevice: %v", err)
	}
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("second WatchDevice: %v", err)
	}
	defer a.Close()

	waitForCount(t, rec, 1, time.Second)
	time.Sleep(80 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Errorf("update count = %d, want exactly 1 — a second WatchDevice call must not start a second poll loop", got)
	}
}

func TestClose_StopsPolling(t *testing.T) {
	ss := &snapshotServer{}
	ss.set(map[string]wireValue{"temperature": {Float: floatPtr(21.5)}})
	srv := httptest.NewServer(ss)
	defer srv.Close()

	rec := &updateRecorder{}
	a := New(rec.onUpdate, WithPollInterval(15*time.Millisecond))
	d := deviceWithEndpoint(srv.URL)
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}
	waitForCount(t, rec, 1, time.Second)

	a.Close()
	before := rec.count()
	time.Sleep(80 * time.Millisecond)
	if after := rec.count(); after != before {
		t.Errorf("updates continued after Close: before=%d after=%d", before, after)
	}
}

func TestWatchDevice_MissingEndpointLabel(t *testing.T) {
	a := New(nil)
	if err := a.WatchDevice(context.Background(), api.Device{ID: "dev-1"}); err == nil {
		t.Fatal("expected error for a device with no http.endpoint label, got nil")
	}
}

func TestWatchDevice_PerDevicePollIntervalOverride(t *testing.T) {
	ss := &snapshotServer{}
	ss.set(map[string]wireValue{"temperature": {Float: floatPtr(21.5)}})
	srv := httptest.NewServer(ss)
	defer srv.Close()

	rec := &updateRecorder{}
	// Adapter-wide default is intentionally slow; the device's own
	// LabelPollInterval override should still make this fast.
	a := New(rec.onUpdate, WithPollInterval(10*time.Second))
	d := deviceWithEndpoint(srv.URL)
	d.Labels[LabelPollInterval] = "15ms"
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}
	defer a.Close()

	waitForCount(t, rec, 1, 500*time.Millisecond)
}
