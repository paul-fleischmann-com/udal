//go:build integration

package canadapter

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// TestIntegration_ReadWrite exercises the Adapter against a real Linux
// SocketCAN interface (issue #25's "Tested against virtual CAN (vcan0) in
// CI"). CI provides one via the go-integration-can job
// (.github/workflows/ci.yml, which runs `ip link add vcan0 type vcan`) and
// sets UDAL_TEST_CAN_INTERFACE; run locally the same way against any
// SocketCAN interface, e.g.:
//
//	sudo modprobe vcan && sudo ip link add dev vcan0 type vcan && sudo ip link set up vcan0
//	UDAL_TEST_CAN_INTERFACE=vcan0 go test -tags integration ./internal/adapters/can/...
//
// The "device" side is simulated with a second, independent raw socket
// bound to the same interface (openSocket — a real SocketCAN socket, not a
// fake), since every socket bound to a CAN interface sees every frame on
// the bus, exactly like a second physical ECU would.
func TestIntegration_ReadWrite(t *testing.T) {
	iface := os.Getenv("UDAL_TEST_CAN_INTERFACE")
	if iface == "" {
		t.Skip("UDAL_TEST_CAN_INTERFACE not set")
	}
	ctx := context.Background()

	f, err := os.Open("testdata/sample.dbc")
	if err != nil {
		t.Fatalf("open testdata: %v", err)
	}
	db, err := ParseDBC(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("ParseDBC: %v", err)
	}
	msg := db.MessageByName("EngineData")

	a := New(db, nil, WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err := a.Open(iface); err != nil {
		t.Fatalf("Open(%q): %v", iface, err)
	}
	defer a.Close()

	dev, err := openSocket(iface)
	if err != nil {
		t.Fatalf("open device-side socket: %v", err)
	}
	defer dev.Close()

	d := api.Device{ID: "dev-1", Transport: "can", Labels: map[string]string{LabelMessage: "EngineData"}}
	if err := a.WatchDevice(ctx, d); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}

	// Device -> gateway: publish a frame from the simulated device side and
	// verify the adapter's read loop picks it up off the real bus.
	var data [8]byte
	if err := msg.Encode(data[:], "EngineSpeed", api.FloatValue(100.0)); err != nil {
		t.Fatalf("encode EngineSpeed: %v", err)
	}
	if err := dev.WriteFrame(Frame{ID: msg.ID, DLC: msg.DLC, Data: data}); err != nil {
		t.Fatalf("device write frame: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var got api.PropertyValue
	for time.Now().Before(deadline) {
		got, err = a.ReadProperty(ctx, d, "EngineSpeed")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("ReadProperty EngineSpeed: %v", err)
	}
	if got.FloatVal == nil || *got.FloatVal != 100.0 {
		t.Fatalf("ReadProperty EngineSpeed = %+v, want 100.0", got)
	}

	// Gateway -> device: WriteProperty and verify the frame actually landed
	// on the bus by reading it back on a freshly-opened device-side socket
	// (fresh, so its receive buffer can't contain anything from earlier in
	// the test — only the write below).
	reader, err := openSocket(iface)
	if err != nil {
		t.Fatalf("open device-side read socket: %v", err)
	}
	defer reader.Close()

	if err := a.WriteProperty(ctx, d, "EngineTemp", api.FloatValue(-10.0)); err != nil {
		t.Fatalf("WriteProperty EngineTemp: %v", err)
	}

	frame, err := reader.ReadFrame()
	if err != nil {
		t.Fatalf("device read frame: %v", err)
	}
	if frame.ID != msg.ID {
		t.Fatalf("frame.ID = %d, want %d", frame.ID, msg.ID)
	}
	decoded := msg.Decode(frame.Data[:])
	v, ok := decoded["EngineTemp"]
	if !ok || v.FloatVal == nil || *v.FloatVal != -10.0 {
		t.Fatalf("decoded frame EngineTemp = %+v, want -10.0", decoded["EngineTemp"])
	}
}
