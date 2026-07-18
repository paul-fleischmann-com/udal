// Package heartbeat implements device online/offline detection (F-04,
// GitHub issue #42): Registry persistence and UpdateStatus already exist
// (#10) — this package is specifically the periodic timeout-detection
// logic that was missing, plus the "mark alive right now" half that every
// heartbeat source (MQTT status topic, an open StreamCommands connection,
// device registration) calls into.
package heartbeat

import (
	"context"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
)

// DefaultInterval and DefaultTimeout match issue #42's acceptance
// criteria ("default 3 x 30s = 90s"), used when gateway.yaml/env don't
// configure gateway.heartbeat_interval/device_timeout (#41).
const (
	DefaultInterval = 30 * time.Second
	DefaultTimeout  = 90 * time.Second
)

// Monitor tracks device liveness against a Registry, emitting a
// PropertyUpdate with Status set through Broker (reusing Subscribe's
// existing fan-out, see #8) on every online/offline transition.
//
// Touch and Sweep each read-then-write a device's status non-atomically;
// a rare double transition/event under concurrent touches for the same
// device is tolerated rather than adding per-device locking, consistent
// with Broker's own "best-effort, skip if full" tolerance elsewhere.
type Monitor struct {
	reg      registry.Registry
	broker   *api.Broker
	interval time.Duration
	timeout  time.Duration

	now func() time.Time // overridable in tests
}

// NewMonitor returns a Monitor that considers a device offline once it's
// gone interval/timeout-silent. A non-positive interval/timeout (zero, or
// negative -- e.g. a "-5s" gateway.yaml value, which time.ParseDuration
// accepts without error) uses DefaultInterval/DefaultTimeout instead of
// propagating into time.NewTicker in Run, which panics on a duration <= 0
// and would otherwise crash the whole gateway process from that goroutine.
func NewMonitor(reg registry.Registry, broker *api.Broker, interval, timeout time.Duration) *Monitor {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Monitor{reg: reg, broker: broker, interval: interval, timeout: timeout, now: time.Now}
}

// Interval returns the configured heartbeat interval — used by callers
// (e.g. StreamCommands' handler) that need to touch presence on the same
// cadence Sweep uses to detect its absence.
func (m *Monitor) Interval() time.Duration { return m.interval }

// Touch records deviceID as alive right now, transitioning it to online
// (and emitting a status event) if it wasn't already.
func (m *Monitor) Touch(deviceID string) error {
	now := m.now()
	d, err := m.reg.Get(deviceID)
	if err != nil {
		return err
	}
	wasOnline := d.Status == api.DeviceStatusOnline
	if err := m.reg.UpdateStatus(deviceID, api.DeviceStatusOnline, now); err != nil {
		return err
	}
	if !wasOnline {
		m.emit(deviceID, api.DeviceStatusOnline, now)
	}
	return nil
}

// Sweep transitions every currently-online device whose last-seen
// timestamp is older than the configured timeout to offline, emitting a
// status event for each. Devices that have never been touched (status
// Unknown) are left alone — there's no online-to-offline transition to
// report for a device that was never confirmed online. Errors updating an
// individual device are swallowed so one bad record doesn't stop the rest
// of the sweep; Sweep only returns an error if listing devices itself
// fails.
func (m *Monitor) Sweep() error {
	now := m.now()
	online := true
	// Filtering to Online devices at the registry layer (rather than
	// listing everything and skipping non-Online ones here) matters once
	// device counts grow (#43's 1,000-device load test) -- every offline
	// or never-touched device would otherwise still cost a full
	// unmarshal/compare on every single sweep interval for no reason.
	devices, err := m.reg.List(registry.ListFilter{Online: &online})
	if err != nil {
		return err
	}
	for _, d := range devices {
		if now.Sub(d.LastSeen) < m.timeout {
			continue
		}
		// LastSeen is preserved as-is (not bumped to now) — the device
		// wasn't just seen, it's being declared offline for having gone
		// quiet since that timestamp.
		if err := m.reg.UpdateStatus(d.ID, api.DeviceStatusOffline, d.LastSeen); err != nil {
			continue
		}
		m.emit(d.ID, api.DeviceStatusOffline, now)
	}
	return nil
}

func (m *Monitor) emit(deviceID string, s api.DeviceStatus, at time.Time) {
	m.broker.Publish(api.PropertyUpdate{DeviceID: deviceID, Timestamp: at, Status: &s})
}

// Run calls Sweep on the configured interval until ctx is done. Intended
// to run in its own goroutine for the gateway process's lifetime.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Sweep()
		}
	}
}
