// Package adaptertest is the common conformance test suite every
// adapter.Transport implementation — built-in or third-party — can run
// against itself (req42.adoc F-12 AC: "Example third-party adapter
// compiles and passes the common adapter test suite", GitHub issue #26).
package adaptertest

import (
	"context"
	"errors"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/adapter"
	"github.com/paulefl/udal/code/gateway/internal/api"
)

// Run exercises the contract every Transport must satisfy. newTransport is
// called once per subtest so each gets a fresh instance — implementations
// that hold connection state (a real MQTT/HTTP client, a socket) may need
// per-instance setup, which newTransport is the hook for.
func Run(t *testing.T, newTransport func() adapter.Transport) {
	t.Helper()

	t.Run("Name is non-empty", func(t *testing.T) {
		if name := newTransport().Name(); name == "" {
			t.Error("Name() returned an empty string — required so metrics/tracing labels and error messages identify this transport")
		}
	})

	t.Run("WatchDevice does not error for a well-formed device", func(t *testing.T) {
		tr := newTransport()
		d := api.Device{ID: "conformance-test-device", Transport: tr.Name()}
		if err := tr.WatchDevice(context.Background(), d); err != nil {
			t.Errorf("WatchDevice(%+v) = %v, want nil for a plain device with no special label requirements", d, err)
		}
	})

	t.Run("WriteProperty then ReadProperty round-trips, or WriteProperty is unsupported", func(t *testing.T) {
		tr := newTransport()
		d := api.Device{ID: "conformance-test-device", Transport: tr.Name()}
		ctx := context.Background()
		want := api.FloatValue(42)

		err := tr.WriteProperty(ctx, d, "conformance-test-property", want)
		if errors.Is(err, adapter.ErrWriteNotSupported) {
			return // read-only transport (e.g. HTTP) — nothing further to check
		}
		if err != nil {
			t.Fatalf("WriteProperty(%+v, ...) = %v, want nil or ErrWriteNotSupported", d, err)
		}

		got, err := tr.ReadProperty(ctx, d, "conformance-test-property")
		if err != nil {
			t.Fatalf("ReadProperty after a successful WriteProperty = %v, want the just-written value with no error", err)
		}
		if got.FloatVal == nil || *got.FloatVal != *want.FloatVal {
			t.Errorf("ReadProperty after WriteProperty = %+v, want %+v", got, want)
		}
	})
}
