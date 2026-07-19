// Package adapter defines the public contract third-party transport
// adapters implement to plug into the gateway (req42.adoc F-12/QR-09,
// GitHub issue #26) — distinct from internal/adapters/{mqtt,http,can},
// which are the gateway's own built-in adapters and don't go through this
// interface or the registry below.
package adapter

import (
	"context"
	"errors"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// Transport is the interface a third-party adapter implements to route
// GetProperty/SetProperty for devices whose Transport field matches its
// registered name (see Register) — the same three operations
// internal/service.DeviceService's built-in MQTTAdapter/HTTPAdapter/
// CANAdapter interfaces already expose individually, unified into one
// public interface so a single DeviceService code path can dispatch to any
// of them, built-in or third-party, without a type switch per adapter.
//
// Implementations should be safe for concurrent use — DeviceService may
// call ReadProperty/WriteProperty for different devices concurrently, same
// as it does for the built-in adapters.
type Transport interface {
	// Name identifies this transport for metrics labels
	// (udal_adapter_errors_total{adapter=...}), tracing, and error
	// messages. Devices route to this Transport when their Transport field
	// equals the name it was Register-ed under.
	Name() string

	// ReadProperty returns d's current value at path.
	ReadProperty(ctx context.Context, d api.Device, path string) (api.PropertyValue, error)

	// WriteProperty sets d's value at path. Read-only transports (compare
	// internal/adapters/http, which has no WriteProperty at all — issue
	// #24's AC don't include one) return ErrWriteNotSupported instead of
	// omitting the method, so every Transport has the same three-method
	// shape regardless of which operations it actually supports.
	WriteProperty(ctx context.Context, d api.Device, path string, v api.PropertyValue) error

	// WatchDevice is called once when d is registered (and, for a device
	// already registered when the transport is wired in at startup, once
	// per such device — see cmd/gateway/main.go) so the transport can start
	// whatever subscription/poll loop it needs to keep d's properties
	// fresh. Best-effort by convention (mirroring the built-in adapters):
	// a failure here shouldn't fail RegisterDevice itself, since
	// ReadProperty/WriteProperty will surface the same underlying problem
	// clearly on the next call.
	WatchDevice(ctx context.Context, d api.Device) error
}

// ErrWriteNotSupported is the error a read-only Transport's WriteProperty
// returns. DeviceService.SetProperty maps it to codes.Unimplemented,
// mirroring how it already special-cases the built-in HTTPAdapter (which
// has no WriteProperty method at all, having been built before this
// interface existed).
var ErrWriteNotSupported = errors.New("adapter: WriteProperty not supported by this transport")

// ErrNotFound and ErrInvalidArgument are sentinel errors a Transport's
// ReadProperty/WriteProperty may wrap (errors.Is-compatible, e.g. via
// fmt.Errorf("...: %w", adapter.ErrNotFound)) to get the same precise gRPC
// status mapping (codes.NotFound/codes.InvalidArgument) the built-in
// mqtt/http/can adapters already have via their own adapter-specific
// sentinels (mqttadapter.ErrInvalidTopicSegment, canadapter.ErrUnknownMessage,
// httpadapter.StatusError, ...) — DeviceService has no way to know a
// third-party Transport's own error types, so without these two, every
// unrecognized error would map to codes.Internal regardless of whether it
// actually represents a routine "no such property" or "bad request"
// condition (code review finding, issue #26). Optional: a Transport that
// doesn't use these still works, just with the coarser Internal default.
var (
	ErrNotFound        = errors.New("adapter: property not found")
	ErrInvalidArgument = errors.New("adapter: invalid argument")
)
