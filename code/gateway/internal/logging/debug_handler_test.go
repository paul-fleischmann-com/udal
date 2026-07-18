package logging

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLevelHandler_Get(t *testing.T) {
	v, err := NewLevelVar("info")
	if err != nil {
		t.Fatalf("NewLevelVar: %v", err)
	}
	h := LevelHandler(v)

	req := httptest.NewRequest(http.MethodGet, "/debug/log-level", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["level"] != "INFO" {
		t.Errorf(`level = %q, want "INFO"`, body["level"])
	}
}

// TestLevelHandler_Put covers F-23 AC: "UDAL_LOG_LEVEL=debug enables debug
// logs without restart" — this endpoint is the actual runtime-adjustment
// path (see LevelHandler's doc comment for why re-reading the env var
// itself can't do this for an already-running process).
func TestLevelHandler_Put(t *testing.T) {
	v, err := NewLevelVar("info")
	if err != nil {
		t.Fatalf("NewLevelVar: %v", err)
	}
	h := LevelHandler(v)

	req := httptest.NewRequest(http.MethodPut, "/debug/log-level", strings.NewReader("debug"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if v.Level() != slog.LevelDebug {
		t.Errorf("Level() after PUT = %v, want debug", v.Level())
	}
}

func TestLevelHandler_Put_InvalidLevel(t *testing.T) {
	v, _ := NewLevelVar("info")
	h := LevelHandler(v)

	req := httptest.NewRequest(http.MethodPut, "/debug/log-level", strings.NewReader("verbose"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if v.Level() != slog.LevelInfo {
		t.Errorf("Level() after invalid PUT = %v, want unchanged (info)", v.Level())
	}
}

func TestLevelHandler_MethodNotAllowed(t *testing.T) {
	v, _ := NewLevelVar("info")
	h := LevelHandler(v)

	req := httptest.NewRequest(http.MethodDelete, "/debug/log-level", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
