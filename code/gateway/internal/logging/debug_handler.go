package logging

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// LevelHandler serves GET/PUT /debug/log-level against level: GET returns
// the current level, PUT (body: "debug"|"info"|"warn"|"error") changes it.
//
// This is F-23's actual "UDAL_LOG_LEVEL=debug enables debug logs without
// restart" mechanism. A running process can't observe a change to its own
// environment variables without being re-exec'd, so literally re-reading
// UDAL_LOG_LEVEL wouldn't accomplish "without restart" — UDAL_LOG_LEVEL
// still sets the level a freshly-started gateway begins at (see
// cmd/gateway/main.go), and this endpoint is the live-adjustment path for
// an already-running one. Mounted on the gateway's metrics listener
// (adapters.metrics_port/UDAL_METRICS_PORT — parsed since issue #41 but
// otherwise still unused until issue #27 adds /health and /metrics to the
// same mux).
func LevelHandler(level *slog.LevelVar) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeLevel(w, level)
		case http.MethodPut, http.MethodPost:
			body, err := io.ReadAll(io.LimitReader(r.Body, 64))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			lvl, err := ParseLevel(strings.TrimSpace(string(body)))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			level.Set(lvl)
			writeLevel(w, level)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func writeLevel(w http.ResponseWriter, level *slog.LevelVar) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"level": level.Level().String()})
}
