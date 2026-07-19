// Package echo is a reference third-party transport adapter (req42.adoc
// F-12/QR-09, GitHub issue #26): a trivial in-memory store that proves the
// adapter.Transport interface and the compiled-in registration path work
// end-to-end, without touching any file under code/gateway/internal/
// service, code/gateway/internal/adapter, or code/gateway/internal/
// adapters/{mqtt,http,can} — the only integration point a gateway operator
// needs is a blank import of this package plus "echo" in gateway.yaml's
// adapters.custom list (see cmd/gateway/main.go).
//
// This package lives under code/gateway/ (not a separate top-level
// module/repo, which a "third-party" adapter would more realistically be)
// because internal/adapter.Transport is, per issue #26's own acceptance
// criterion, an internal package — Go's internal-import visibility rule
// means only code rooted under code/gateway/ can import it at all. That
// matches QR-09's actual stimulus text ("Contributor adds J1939 adapter"):
// pluggability for a contributor to this repository, not for a genuinely
// external, separately-versioned module.
//
// It isn't a real transport — WriteProperty just remembers the value
// in-process for the next ReadProperty, there is no device on the other
// end of anything. Useful as a template for a real third-party adapter
// (e.g. a J1939 CAN dialect, req42.adoc QR-09's example scenario) and as
// the subject of internal/adapter/adaptertest's conformance suite (see
// echo_test.go).
package echo

import (
	"context"
	"fmt"
	"sync"

	"github.com/paulefl/udal/code/gateway/internal/adapter"
	"github.com/paulefl/udal/code/gateway/internal/api"
)

// Name is the transport name this package registers itself under —
// gateway.yaml's adapters.custom list and a device's Transport field both
// reference it by this string.
const Name = "echo"

func init() {
	adapter.Register(Name, New())
}

// Adapter implements adapter.Transport by echoing back whatever was last
// written, per device+property, entirely in-memory.
type Adapter struct {
	mu   sync.RWMutex
	data map[string]api.PropertyValue // key: deviceID + "/" + path
}

// New returns a ready-to-use Adapter. Exported (rather than only reachable
// via the package-level singleton init() registers) so a test — or a
// gateway embedding this package directly instead of through the registry
// — can construct its own isolated instance.
func New() *Adapter {
	return &Adapter{data: make(map[string]api.PropertyValue)}
}

func (a *Adapter) Name() string { return Name }

func (a *Adapter) key(deviceID, path string) string {
	return deviceID + "/" + path
}

// ReadProperty returns the value last written for d/path. Returns an error
// if nothing has been written yet — there's no hardware default to fall
// back to, same reasoning as api.MemoryPropertyStore.Get.
func (a *Adapter) ReadProperty(_ context.Context, d api.Device, path string) (api.PropertyValue, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	v, ok := a.data[a.key(d.ID, path)]
	if !ok {
		return api.PropertyValue{}, fmt.Errorf("echo: property %q not set on device %q", path, d.ID)
	}
	return v, nil
}

// WriteProperty stores v as d/path's new value, visible to the next
// ReadProperty.
func (a *Adapter) WriteProperty(_ context.Context, d api.Device, path string, v api.PropertyValue) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data[a.key(d.ID, path)] = v
	return nil
}

// WatchDevice is a no-op — this adapter has no subscription/poll loop to
// start, values only ever change via WriteProperty.
func (a *Adapter) WatchDevice(_ context.Context, _ api.Device) error { return nil }
