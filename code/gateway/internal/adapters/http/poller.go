package httpadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// pollLoop periodically GETs deviceID's bulk property snapshot
// (snapshotURL) and calls onUpdate for each property whose encoded value
// changed since the previous tick — this is what keeps a Subscribe stream
// live for an http-transport device that never calls the webhook (issue
// #24 AC: "Poll interval configurable per device"). Runs until ctx is
// cancelled (see Adapter.Close).
func (a *Adapter) pollLoop(ctx context.Context, deviceID, endpoint string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	last := make(map[string]string) // path -> snapshotKey of the last value seen
	a.poll(ctx, deviceID, endpoint, last)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.poll(ctx, deviceID, endpoint, last)
		}
	}
}

// poll performs one snapshot GET and fires onUpdate for changed properties,
// updating last in place. Errors (network, non-2xx, malformed body) are
// logged and otherwise swallowed — a single failed tick shouldn't stop the
// loop, the next tick will simply try again.
func (a *Adapter) poll(ctx context.Context, deviceID, endpoint string, last map[string]string) {
	reqCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, snapshotURL(endpoint), nil)
	if err != nil {
		a.log.Warn("http: build poll request", "device", deviceID, "err", err)
		return
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.log.Warn("http: poll failed", "device", deviceID, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		a.log.Warn("http: poll returned error status", "device", deviceID, "status", resp.Status)
		return
	}
	var snapshot map[string]wireValue
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		a.log.Warn("http: decode poll snapshot", "device", deviceID, "err", err)
		return
	}

	for path, w := range snapshot {
		key := snapshotKey(w)
		if last[path] == key {
			continue
		}
		last[path] = key
		if a.onUpdate != nil {
			a.onUpdate(deviceID, path, decodeValue(w))
		}
	}
}
