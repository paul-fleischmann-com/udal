// Package health implements the gateway's readiness endpoint (req42.adoc
// F-21, GitHub issue #27): GET /health returns 503 while the gateway is
// still starting up, and 200 once it's ready — with a per-adapter
// "degraded" status in the body if a registered Reporter reports one,
// without dropping the overall response to non-200 (an adapter being
// unhealthy doesn't mean the gateway process itself isn't ready to serve
// requests against everything else).
package health

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Reporter is implemented by anything that can report its own health —
// currently the MQTT and CAN adapters (see their Healthy() methods); the
// HTTP adapter doesn't implement it, since it has no persistent connection
// or comparable failure mode to report (each request carries its own
// timeout, see package httpadapter's doc comment on why it has no circuit
// breaker either).
type Reporter interface {
	// Healthy reports whether the Reporter is currently healthy, and a
	// human-readable detail (ignored when ok is true).
	Healthy() (ok bool, detail string)
}

// Checker tracks the gateway's readiness and a set of named Reporters,
// backing the /health endpoint (see Handler).
type Checker struct {
	mu    sync.RWMutex
	ready bool
	rep   map[string]Reporter
}

// NewChecker returns a Checker that starts not-ready (see SetReady).
func NewChecker() *Checker {
	return &Checker{rep: make(map[string]Reporter)}
}

// SetReady marks the gateway ready (or not) to serve requests — main.go
// calls SetReady(true) once every listener is up, so a load balancer's
// readiness probe doesn't route traffic to the gateway mid-startup.
func (c *Checker) SetReady(ready bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready = ready
}

// Register adds a named Reporter (e.g. "mqtt_adapter") whose Healthy()
// result is included in /health's response body once the gateway is
// ready. Not safe to call concurrently with Handler serving a request for
// the same name (main.go registers everything during startup, before
// SetReady(true) and before the metrics listener starts accepting
// connections).
func (c *Checker) Register(name string, r Reporter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rep[name] = r
}

type adapterStatus struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type response struct {
	Status   string                   `json:"status"`
	Adapters map[string]adapterStatus `json:"adapters,omitempty"`
}

// Handler serves GET /health (req42.adoc F-21 AC):
//   - not ready: 503 {"status":"starting"}
//   - ready: 200 {"status":"ok"[,"adapters":{...}]} — "adapters" is present
//     only when at least one Reporter is registered; each entry is
//     "status":"ok" or "status":"degraded" (+ "detail") per Reporter,
//     independent of the overall 200 (an unhealthy adapter doesn't fail
//     the whole gateway's readiness).
func (c *Checker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		c.mu.RLock()
		ready := c.ready
		reporters := make(map[string]Reporter, len(c.rep))
		for name, rep := range c.rep {
			reporters[name] = rep
		}
		c.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(response{Status: "starting"})
			return
		}

		resp := response{Status: "ok"}
		if len(reporters) > 0 {
			resp.Adapters = make(map[string]adapterStatus, len(reporters))
			for name, rep := range reporters {
				if ok, detail := rep.Healthy(); ok {
					resp.Adapters[name] = adapterStatus{Status: "ok"}
				} else {
					resp.Adapters[name] = adapterStatus{Status: "degraded", Detail: detail}
				}
			}
		}
		// Always 200 once ready, even with degraded adapters (F-21 AC:
		// "Adapter(s) failed -> 200 with degraded status per adapter").
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}
