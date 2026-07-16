package mqtt

import (
	"context"
	"errors"
	"testing"
)

// fakeTransport is a minimal transport for exercising Adapter logic without
// a real broker.
type fakeTransport struct {
	disconnected bool
}

func (f *fakeTransport) Publish(context.Context, string, []byte) error { return nil }
func (f *fakeTransport) Subscribe(context.Context, string) error       { return nil }
func (f *fakeTransport) Disconnect(context.Context) error              { f.disconnected = true; return nil }

func TestAdapter_ConnectFallsBackToV3OnUnsupportedVersion(t *testing.T) {
	// No real v3.1.1-only broker is available to trigger this path against
	// actual infra (Mosquitto accepts v5 fine — see plan doc); connectV5
	// itself is exercised for real against Mosquitto elsewhere. This test
	// covers Connect's fallback *decision* with a fake v5 connector that
	// simulates the CONNACK "unsupported protocol version" rejection.
	v5Calls, v3Calls := 0, 0
	a := New("mqtt://example.invalid:1883", nil)
	a.connectV5 = func(context.Context, string, func(string, []byte)) (transport, error) {
		v5Calls++
		return nil, errUnsupportedVersion
	}
	a.connectV3 = func(context.Context, string, func(string, []byte)) (transport, error) {
		v3Calls++
		return &fakeTransport{}, nil
	}

	if err := a.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if v5Calls != 1 || v3Calls != 1 {
		t.Fatalf("v5Calls=%d v3Calls=%d, want 1 and 1", v5Calls, v3Calls)
	}
}

func TestAdapter_ConnectDoesNotFallBackOnOtherErrors(t *testing.T) {
	// A v5 connect failure unrelated to protocol version (e.g. network
	// unreachable) must be returned as-is, not silently retried as v3.1.1.
	wantErr := errors.New("network unreachable")
	v3Calls := 0
	a := New("mqtt://example.invalid:1883", nil)
	a.connectV5 = func(context.Context, string, func(string, []byte)) (transport, error) {
		return nil, wantErr
	}
	a.connectV3 = func(context.Context, string, func(string, []byte)) (transport, error) {
		v3Calls++
		return &fakeTransport{}, nil
	}

	err := a.Connect(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Connect error = %v, want wrapping %v", err, wantErr)
	}
	if v3Calls != 0 {
		t.Fatalf("v3 fallback should not be attempted for non-version errors, got %d calls", v3Calls)
	}
}

func TestAdapter_ConnectSucceedsWithV5(t *testing.T) {
	v3Calls := 0
	a := New("mqtt://example.invalid:1883", nil)
	a.connectV5 = func(context.Context, string, func(string, []byte)) (transport, error) {
		return &fakeTransport{}, nil
	}
	a.connectV3 = func(context.Context, string, func(string, []byte)) (transport, error) {
		v3Calls++
		return nil, errors.New("should not be called")
	}

	if err := a.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if v3Calls != 0 {
		t.Fatalf("v3 fallback attempted despite v5 success, %d calls", v3Calls)
	}
}
