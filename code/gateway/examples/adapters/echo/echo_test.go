package echo_test

import (
	"testing"

	"github.com/paulefl/udal/code/gateway/examples/adapters/echo"
	"github.com/paulefl/udal/code/gateway/internal/adapter"
	"github.com/paulefl/udal/code/gateway/internal/adapter/adaptertest"
)

// TestEcho_ConformsToTransport is req42.adoc F-12's AC: "Example
// third-party adapter compiles and passes the common adapter test suite".
func TestEcho_ConformsToTransport(t *testing.T) {
	adaptertest.Run(t, func() adapter.Transport { return echo.New() })
}

func TestEcho_RegistersItselfUnderItsName(t *testing.T) {
	tr, ok := adapter.Lookup(echo.Name)
	if !ok {
		t.Fatalf("adapter.Lookup(%q) = not found; echo's init() should have registered it on import", echo.Name)
	}
	if tr.Name() != echo.Name {
		t.Errorf("registered transport's Name() = %q, want %q", tr.Name(), echo.Name)
	}
}
