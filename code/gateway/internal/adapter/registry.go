package adapter

import (
	"fmt"
	"sync"
)

// registry holds every Transport a compiled-in adapter package has
// Register-ed via its own init() — the "compiled-in registration" path
// req42.adoc F-12's third AC offers as an alternative to a binary/.so
// plugin. A third-party adapter package (see code/gateway/examples/
// adapters/echo for the reference example) needs no changes to any
// existing gateway or adapters/ package to add itself here: its own
// init() calls Register, and cmd/gateway/main.go picks it up by name from
// adapters.custom in gateway.yaml (or UDAL_CUSTOM_ADAPTERS) — the only
// integration point is one blank import of the new adapter package in
// main.go, the same single-line integration every Go program using this
// registration idiom needs (e.g. database/sql drivers).
//
// A Go-native plugin.Open(".so") loader was considered and deliberately
// not built: plugins must be compiled with the exact same Go toolchain
// version and build flags as the host binary, are Linux-only, and would
// undermine req42.adoc QR-07's "single binary" portability goal by
// requiring a separately-distributed, ABI-fragile artifact per adapter.
var (
	registryMu sync.RWMutex
	registry   = map[string]Transport{}
)

// Register makes t available under name for gateway.yaml's
// adapters.custom list (or UDAL_CUSTOM_ADAPTERS) to activate. Intended to
// be called from an adapter package's own init(), so blank-importing the
// package (`_ "import/path"`) is enough to make it available — see
// code/gateway/examples/adapters/echo/echo.go. Panics on a duplicate name
// or a nil t, same as database/sql's driver registry: both are build-time
// programming errors in the registering package's own init(), not runtime
// conditions to recover from — better to panic here, at process startup,
// with a clear message naming the transport, than to let a nil Transport
// sit silently in the registry and panic on nil-interface method dispatch
// later, mid-request, once some device's Transport field happens to match.
func Register(name string, t Transport) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if t == nil {
		panic(fmt.Sprintf("adapter: Register called with a nil Transport for %q", name))
	}
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("adapter: Register called twice for transport %q", name))
	}
	registry[name] = t
}

// Lookup returns the Transport registered under name, if any.
// cmd/gateway/main.go calls this once per name listed in
// adapters.custom/UDAL_CUSTOM_ADAPTERS at startup.
func Lookup(name string) (Transport, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	t, ok := registry[name]
	return t, ok
}
