package canadapter

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// openFake wires a fakeSocket into a, bypassing Open/openSocket (no real
// interface needed), and starts the read loop against it — mirroring how
// mqtt's adapter_faketransport_test.go bypasses Connect.
func openFake(a *Adapter) *fakeSocket {
	sock := newFakeSocket()
	a.mu.Lock()
	a.sock = sock
	a.mu.Unlock()
	go a.readLoop(sock)
	return sock
}

func newTestAdapter(t *testing.T, onUpdate OnPropertyUpdate) (*Adapter, *fakeSocket) {
	t.Helper()
	db := loadTestDB(t)
	a := New(db, onUpdate)
	sock := openFake(a)
	t.Cleanup(func() { _ = a.Close() })
	return a, sock
}

// waitFor polls cond until it's true or timeout elapses, failing the test
// otherwise — used to synchronize against the adapter's async read loop.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func engineDevice() api.Device {
	return api.Device{ID: "dev-1", Transport: "can", Labels: map[string]string{LabelMessage: "EngineData"}}
}

func TestAdapter_WatchDevice_MissingLabel(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	err := a.WatchDevice(context.Background(), api.Device{ID: "dev-1"})
	if !errors.Is(err, ErrMissingLabel) {
		t.Errorf("err = %v, want ErrMissingLabel", err)
	}
}

func TestAdapter_WatchDevice_UnknownMessage(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	d := api.Device{ID: "dev-1", Labels: map[string]string{LabelMessage: "NoSuchMessage"}}
	err := a.WatchDevice(context.Background(), d)
	if !errors.Is(err, ErrUnknownMessage) {
		t.Errorf("err = %v, want ErrUnknownMessage", err)
	}
}

func TestAdapter_ReadProperty_NoFrameYet(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	d := engineDevice()
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}
	_, err := a.ReadProperty(context.Background(), d, "EngineSpeed")
	if !errors.Is(err, ErrNoFrameYet) {
		t.Errorf("err = %v, want ErrNoFrameYet", err)
	}
}

func TestAdapter_ReadProperty_UnknownSignal(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	d := engineDevice()
	_, err := a.ReadProperty(context.Background(), d, "NoSuchSignal")
	if !errors.Is(err, ErrUnknownSignal) {
		t.Errorf("err = %v, want ErrUnknownSignal", err)
	}
}

// TestAdapter_ReadProperty_FromBusFrame delivers a raw frame through the
// fake socket (as if it arrived unsolicited on the bus) and verifies
// ReadProperty answers from the decoded cache (F-11 AC: "returns decoded
// signal value from last received CAN frame").
func TestAdapter_ReadProperty_FromBusFrame(t *testing.T) {
	var mu sync.Mutex
	var updates []string
	a, sock := newTestAdapter(t, func(deviceID, path string, v api.PropertyValue) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, deviceID+"/"+path)
	})
	d := engineDevice()
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}

	sock.deliver(Frame{ID: 256, DLC: 8, Data: [8]byte{0x90, 0x01, 30, 0, 0, 0, 0, 0}})

	waitFor(t, func() bool {
		v, err := a.ReadProperty(context.Background(), d, "EngineSpeed")
		return err == nil && v.FloatVal != nil && *v.FloatVal == 100.0
	})

	v, err := a.ReadProperty(context.Background(), d, "EngineTemp")
	if err != nil {
		t.Fatalf("ReadProperty EngineTemp: %v", err)
	}
	if v.FloatVal == nil || *v.FloatVal != -10.0 {
		t.Errorf("EngineTemp = %+v, want -10.0", v)
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(updates) >= 2
	})
}

// TestAdapter_WriteProperty verifies WriteProperty encodes and writes a
// frame, and that ReadProperty immediately reflects it via the same
// read-modify-write/cache path a bus frame would take.
func TestAdapter_WriteProperty(t *testing.T) {
	a, sock := newTestAdapter(t, nil)
	d := engineDevice()

	if err := a.WriteProperty(context.Background(), d, "EngineTemp", api.FloatValue(-10.0)); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}

	written := sock.writtenFrames()
	if len(written) != 1 {
		t.Fatalf("written frames = %d, want 1", len(written))
	}
	if written[0].ID != 256 {
		t.Errorf("written frame ID = %d, want 256", written[0].ID)
	}
	if written[0].Data[2] != 30 {
		t.Errorf("written frame data[2] = %d, want 30 (raw for -10 degC)", written[0].Data[2])
	}

	v, err := a.ReadProperty(context.Background(), d, "EngineTemp")
	if err != nil {
		t.Fatalf("ReadProperty after WriteProperty: %v", err)
	}
	if v.FloatVal == nil || *v.FloatVal != -10.0 {
		t.Errorf("ReadProperty after write = %+v, want -10.0", v)
	}
}

// TestAdapter_WriteProperty_PreservesSiblingSignal writes EngineSpeed then
// EngineTemp and checks the second write didn't clobber the first (both
// signals share EngineData's payload) — the read-modify-write guarantee
// documented on Adapter.WriteProperty.
func TestAdapter_WriteProperty_PreservesSiblingSignal(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	d := engineDevice()

	if err := a.WriteProperty(context.Background(), d, "EngineSpeed", api.FloatValue(100.0)); err != nil {
		t.Fatalf("WriteProperty EngineSpeed: %v", err)
	}
	if err := a.WriteProperty(context.Background(), d, "EngineTemp", api.FloatValue(-10.0)); err != nil {
		t.Fatalf("WriteProperty EngineTemp: %v", err)
	}

	v, err := a.ReadProperty(context.Background(), d, "EngineSpeed")
	if err != nil {
		t.Fatalf("ReadProperty EngineSpeed: %v", err)
	}
	if v.FloatVal == nil || *v.FloatVal != 100.0 {
		t.Errorf("EngineSpeed after sibling write = %+v, want 100.0 (must survive read-modify-write)", v)
	}
}

func TestAdapter_WriteProperty_NotOpen(t *testing.T) {
	db := loadTestDB(t)
	a := New(db, nil) // never Open'd / openFake'd
	err := a.WriteProperty(context.Background(), engineDevice(), "EngineSpeed", api.FloatValue(1))
	if !errors.Is(err, ErrNotOpen) {
		t.Errorf("err = %v, want ErrNotOpen", err)
	}
}

// TestAdapter_UnknownFrameIgnored delivers a frame with an ID absent from
// the DBC file and checks it's silently dropped (no cache entry, no panic,
// no fan-out) rather than erroring the read loop.
func TestAdapter_UnknownFrameIgnored(t *testing.T) {
	var mu sync.Mutex
	var fired []string
	a, sock := newTestAdapter(t, func(deviceID, path string, v api.PropertyValue) {
		mu.Lock()
		defer mu.Unlock()
		fired = append(fired, deviceID+"/"+path)
	})
	d := engineDevice()
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}

	sock.deliver(Frame{ID: 999, DLC: 8})
	// Follow up with a known frame; once *that* one's visible, the unknown
	// one (delivered first, same single-reader queue) has definitely already
	// been processed one way or the other.
	sock.deliver(Frame{ID: 256, DLC: 8, Data: [8]byte{0x90, 0x01, 30, 0, 0, 0, 0, 0}})
	waitFor(t, func() bool {
		_, err := a.ReadProperty(context.Background(), d, "EngineSpeed")
		return err == nil
	})
	mu.Lock()
	defer mu.Unlock()
	for _, f := range fired {
		if f != "dev-1/EngineSpeed" && f != "dev-1/EngineTemp" {
			t.Errorf("unexpected fan-out %q from the unknown-ID frame", f)
		}
	}
}

// TestAdapter_ReadLoop_RecoversFromPanic proves a panic while processing one
// frame (here, deliberately triggered from onUpdate) doesn't kill the read
// loop — arc42.adoc's documented risk ("CAN adapter panic brings down
// entire gateway"). A second, good frame delivered afterward must still be
// processed.
func TestAdapter_ReadLoop_RecoversFromPanic(t *testing.T) {
	first := true
	a, sock := newTestAdapter(t, func(deviceID, path string, v api.PropertyValue) {
		if first {
			first = false
			panic("simulated decode bug")
		}
	})
	d := engineDevice()
	if err := a.WatchDevice(context.Background(), d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}

	sock.deliver(Frame{ID: 256, DLC: 8, Data: [8]byte{0x90, 0x01, 30, 0, 0, 0, 0, 0}})
	waitFor(t, func() bool {
		_, err := a.ReadProperty(context.Background(), d, "EngineSpeed")
		return err == nil
	})

	sock.deliver(Frame{ID: 256, DLC: 8, Data: [8]byte{0xA0, 0x01, 30, 0, 0, 0, 0, 0}}) // 0x01A0=416*0.25=104
	waitFor(t, func() bool {
		v, err := a.ReadProperty(context.Background(), d, "EngineSpeed")
		return err == nil && v.FloatVal != nil && *v.FloatVal == 104.0
	})
}

func TestAdapter_Close_Idempotent(t *testing.T) {
	a, _ := newTestAdapter(t, nil)
	if err := a.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
