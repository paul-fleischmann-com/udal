package httpadapter

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhook_KnownDeviceDispatchesUpdate(t *testing.T) {
	rec := &updateRecorder{}
	a := New(rec.onUpdate)
	a.mu.Lock()
	a.webhookDevices["dev-1"] = struct{}{}
	a.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/devices/dev-1/events",
		bytes.NewBufferString(`{"path":"temperature","value":{"float":23.4}}`))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
	if rec.count() != 1 {
		t.Fatalf("update count = %d, want 1", rec.count())
	}
	got := rec.snapshot()[0]
	if got.deviceID != "dev-1" || got.path != "temperature" || got.value.FloatVal == nil || *got.value.FloatVal != 23.4 {
		t.Errorf("update = %+v, want dev-1/temperature=23.4", got)
	}
}

func TestWebhook_UnknownDeviceReturns404(t *testing.T) {
	rec := &updateRecorder{}
	a := New(rec.onUpdate)

	req := httptest.NewRequest(http.MethodPost, "/devices/unknown-device/events",
		bytes.NewBufferString(`{"path":"temperature","value":{"float":1}}`))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if rec.count() != 0 {
		t.Errorf("update count = %d, want 0 for an unwatched device", rec.count())
	}
}

func TestWebhook_MalformedBodyReturns400(t *testing.T) {
	rec := &updateRecorder{}
	a := New(rec.onUpdate)
	a.mu.Lock()
	a.webhookDevices["dev-1"] = struct{}{}
	a.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/devices/dev-1/events", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestWebhook_MissingPathReturns400(t *testing.T) {
	rec := &updateRecorder{}
	a := New(rec.onUpdate)
	a.mu.Lock()
	a.webhookDevices["dev-1"] = struct{}{}
	a.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/devices/dev-1/events", bytes.NewBufferString(`{"value":{"float":1}}`))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestWebhook_RegisteredByWatchDevice confirms WatchDevice — not just a
// test manually poking the internal map — is what makes a device's webhook
// events accepted, i.e. the real registration path used in production.
func TestWebhook_RegisteredByWatchDevice(t *testing.T) {
	pollSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer pollSrv.Close()

	rec := &updateRecorder{}
	a := New(rec.onUpdate)
	defer a.Close()
	d := deviceWithEndpoint(pollSrv.URL)
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/devices/dev-1/events",
		bytes.NewBufferString(`{"path":"temperature","value":{"float":1}}`))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}
