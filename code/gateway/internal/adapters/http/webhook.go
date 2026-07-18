package httpadapter

import (
	"encoding/json"
	"net/http"
)

// webhookEvent is the JSON body a device POSTs to push one property update
// ahead of its next scheduled poll (issue #24 AC: "webhook receiver starts
// a per-device listener; events forwarded to router").
type webhookEvent struct {
	Path  string    `json:"path"`
	Value wireValue `json:"value"`
}

// Handler returns the HTTP handler for the device-facing webhook receiver.
// main.go mounts it on its own listener (config: adapters.http.webhook_port
// / UDAL_HTTP_WEBHOOK_ADDR) — a separate server from the client-facing
// gRPC/REST gateway, since webhook callers are devices, not the API
// clients the REST gateway's auth/TLS posture is built around.
func (a *Adapter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /devices/{deviceId}/events", a.handleWebhookEvent)
	return mux
}

// handleWebhookEvent accepts a push only for a device that's been
// WatchDevice'd — an unknown deviceId gets 404 rather than silently
// accepting (and discarding) events for a device the gateway never
// registered a poll loop or fan-out target for.
func (a *Adapter) handleWebhookEvent(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("deviceId")

	a.mu.Lock()
	_, watched := a.webhookDevices[deviceID]
	a.mu.Unlock()
	if !watched {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	var ev webhookEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "malformed event body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if ev.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	if a.onUpdate != nil {
		a.onUpdate(deviceID, ev.Path, decodeValue(ev.Value))
	}
	w.WriteHeader(http.StatusNoContent)
}
