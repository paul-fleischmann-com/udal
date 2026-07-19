package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeReporter struct {
	ok     bool
	detail string
}

func (f fakeReporter) Healthy() (bool, string) { return f.ok, f.detail }

func decodeHealth(t *testing.T, rec *httptest.ResponseRecorder) response {
	t.Helper()
	var r response
	if err := json.NewDecoder(rec.Body).Decode(&r); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return r
}

// TestHandler_NotReady covers F-21 AC: "Gateway starting (not yet ready) ->
// 503".
func TestHandler_NotReady(t *testing.T) {
	c := NewChecker()
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if r := decodeHealth(t, rec); r.Status != "starting" {
		t.Errorf(`status field = %q, want "starting"`, r.Status)
	}
}

// TestHandler_ReadyNoAdapters covers F-21 AC: "Gateway ready -> GET /health
// returns 200 {"status": "ok"}".
func TestHandler_ReadyNoAdapters(t *testing.T) {
	c := NewChecker()
	c.SetReady(true)
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	r := decodeHealth(t, rec)
	if r.Status != "ok" {
		t.Errorf(`status field = %q, want "ok"`, r.Status)
	}
	if r.Adapters != nil {
		t.Errorf("adapters = %v, want nil/omitted when none registered", r.Adapters)
	}
}

func TestHandler_ReadyAllHealthy(t *testing.T) {
	c := NewChecker()
	c.SetReady(true)
	c.Register("mqtt_adapter", fakeReporter{ok: true})
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	r := decodeHealth(t, rec)
	if got := r.Adapters["mqtt_adapter"]; got.Status != "ok" {
		t.Errorf("adapters[mqtt_adapter] = %+v, want status=ok", got)
	}
}

// TestHandler_ReadyDegradedAdapter covers F-21 AC: "Adapter(s) failed ->
// 200 with degraded status per adapter in body" — the overall response
// must stay 200 even though an adapter is unhealthy.
func TestHandler_ReadyDegradedAdapter(t *testing.T) {
	c := NewChecker()
	c.SetReady(true)
	c.Register("mqtt_adapter", fakeReporter{ok: false, detail: "circuit breaker open"})
	c.Register("can_adapter", fakeReporter{ok: true})
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (even with a degraded adapter)", rec.Code)
	}
	r := decodeHealth(t, rec)
	if r.Status != "ok" {
		t.Errorf(`top-level status = %q, want "ok"`, r.Status)
	}
	mqtt := r.Adapters["mqtt_adapter"]
	if mqtt.Status != "degraded" || mqtt.Detail != "circuit breaker open" {
		t.Errorf("adapters[mqtt_adapter] = %+v, want status=degraded, detail=\"circuit breaker open\"", mqtt)
	}
	can := r.Adapters["can_adapter"]
	if can.Status != "ok" {
		t.Errorf("adapters[can_adapter] = %+v, want status=ok", can)
	}
}

func TestChecker_SetReady_Toggle(t *testing.T) {
	c := NewChecker()
	c.SetReady(true)
	c.SetReady(false)

	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 after SetReady(false)", rec.Code)
	}
}

// TestHandler_MethodNotAllowed covers a fix from review: Handler() must
// reject non-GET methods, matching logging.LevelHandler's behavior on the
// same metrics mux, rather than serving the health body for any verb.
func TestHandler_MethodNotAllowed(t *testing.T) {
	c := NewChecker()
	c.SetReady(true)
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
