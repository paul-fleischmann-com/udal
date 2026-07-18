// Package canadapter implements the CAN transport adapter (req42.adoc
// F-11, GitHub issue #25): raw CAN frames are read from and written to a
// Linux SocketCAN interface (net/can, req42.adoc TC-01 — Linux-only, see
// socket_linux.go/socket_other.go), decoded to/from typed property values
// using a DBC file loaded once at startup (see dbc.go).
//
// Message convention — a device's DBC message comes from
// Device.Labels[LabelMessage] (there is no separate per-device config
// field on Device; Labels is the existing extensibility mechanism, same
// pattern as httpadapter's LabelEndpoint), and property_path names a signal
// within that message:
//
//	Device.Labels["can.message"] = "EngineData"  // a BO_ name in the DBC file
//	GetProperty(deviceId, "EngineSpeed")          // an SG_ name in that message
//
// Unlike MQTT/HTTP, CAN is a broadcast bus shared by every device on it —
// one DBC file describes every message any device on the interface might
// send, not a per-device schema. The adapter therefore decodes every frame
// matching a known DBC message ID as soon as it arrives (one shared read
// loop per interface), regardless of whether any device has called
// WatchDevice for it yet; ReadProperty always answers from that cache
// (F-11 AC: "returns decoded signal value from last received CAN frame"),
// never by sending a live request — CAN has no request/response semantics
// for arbitrary signals. WatchDevice's role is narrower than MQTT/HTTP's:
// it validates a device's can.message label resolves to a known message and
// registers it for OnPropertyUpdate fan-out, since the read loop otherwise
// has no notion of which decoded signals belong to which registered
// device.
//
// A panic while decoding one frame (e.g. a future bug in a hand-rolled
// codec path) is recovered per-frame in the read loop, not just logged
// after the fact — the risk arc42.adoc calls out is a single bad frame
// taking down the whole structured-monolith gateway process, not just this
// adapter.
package canadapter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// LabelMessage is the api.Device.Labels key holding the DBC message (BO_)
// name a can-transport device's properties belong to. Required for
// WatchDevice/ReadProperty/WriteProperty to do anything.
const LabelMessage = "can.message"

// OnPropertyUpdate is called for every signal value decoded from a frame
// belonging to a watched device's message — both the frame's other signals
// alongside one just written by WriteProperty, and any frame observed
// unsolicited on the bus — so the gateway can fan it out via api.Broker
// (Subscribe RPC). Must not block.
type OnPropertyUpdate func(deviceID, propertyPath string, v api.PropertyValue)

// Option configures an Adapter constructed by New.
type Option func(*Adapter)

// WithLogger overrides the Adapter's logger (default: slog.Default()).
func WithLogger(log *slog.Logger) Option { return func(a *Adapter) { a.log = log } }

// Adapter is the CAN transport adapter. Construct with New, Open an
// interface, wire into DeviceService (service.SetCANAdapter), then call
// WatchDevice for every can-transport device.
type Adapter struct {
	db       *Database
	onUpdate OnPropertyUpdate
	log      *slog.Logger

	mu      sync.RWMutex
	sock    rawSocket
	values  map[string]api.PropertyValue // "message/signal" -> last decoded value
	frames  map[uint32]Frame             // message ID -> last raw frame (read-modify-write base for WriteProperty)
	watched map[string]api.Device        // deviceID -> device, for fan-out message-name matching

	closeOnce sync.Once
	done      chan struct{}
}

// New returns an Adapter for db (see ParseDBC). onUpdate is called for
// every property value the read loop or WriteProperty observes for a
// watched device; it must not block. Call Open before using the adapter.
func New(db *Database, onUpdate OnPropertyUpdate, opts ...Option) *Adapter {
	a := &Adapter{
		db:       db,
		onUpdate: onUpdate,
		log:      slog.Default(),
		values:   make(map[string]api.PropertyValue),
		frames:   make(map[uint32]Frame),
		watched:  make(map[string]api.Device),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Open binds to iface (e.g. "can0", "vcan0") and starts the shared
// background read loop that decodes every incoming frame matching a known
// DBC message. Returns ErrLinuxOnly on non-Linux platforms (TC-01).
func (a *Adapter) Open(iface string) error {
	sock, err := openSocket(iface)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.sock = sock
	a.mu.Unlock()
	go a.readLoop(sock)
	return nil
}

// Close stops the read loop and closes the underlying socket. Safe to call
// more than once.
func (a *Adapter) Close() error {
	a.mu.Lock()
	sock := a.sock
	a.sock = nil
	a.mu.Unlock()
	a.closeOnce.Do(func() { close(a.done) })
	if sock == nil {
		return nil
	}
	return sock.Close()
}

// WatchDevice validates d's can.message label resolves to a message in the
// loaded DBC file and registers d so the read loop's OnPropertyUpdate
// fan-out includes it. Idempotent.
func (a *Adapter) WatchDevice(_ context.Context, d api.Device) error {
	msgName := d.Labels[LabelMessage]
	if msgName == "" {
		return fmt.Errorf("%w: device %q", ErrMissingLabel, d.ID)
	}
	if a.db.MessageByName(msgName) == nil {
		return fmt.Errorf("%w: %q (device %q)", ErrUnknownMessage, msgName, d.ID)
	}
	a.mu.Lock()
	a.watched[d.ID] = d
	a.mu.Unlock()
	return nil
}

// ReadProperty returns the decoded value of path (a signal name) for d's
// resolved DBC message, from the last frame received with that message's
// ID — never a live bus request (see package doc comment). Returns
// ErrNoFrameYet if no matching frame has arrived since Open.
func (a *Adapter) ReadProperty(ctx context.Context, d api.Device, path string) (api.PropertyValue, error) {
	if err := ctx.Err(); err != nil {
		return api.PropertyValue{}, err
	}
	msg, _, err := a.resolve(d, path)
	if err != nil {
		return api.PropertyValue{}, err
	}
	a.mu.RLock()
	v, ok := a.values[msg.Name+"/"+path]
	a.mu.RUnlock()
	if !ok {
		return api.PropertyValue{}, fmt.Errorf("%w: message %q, signal %q", ErrNoFrameYet, msg.Name, path)
	}
	return v, nil
}

// WriteProperty encodes v into path's bit range of d's resolved DBC
// message and writes the resulting frame to the CAN interface. The
// message's other signals are preserved via read-modify-write against the
// last frame seen for that message ID (or an all-zero payload if none has
// been seen yet), so writing one signal never clobbers its siblings sharing
// the same frame.
func (a *Adapter) WriteProperty(ctx context.Context, d api.Device, path string, v api.PropertyValue) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	msg, _, err := a.resolve(d, path)
	if err != nil {
		return err
	}
	a.mu.RLock()
	sock := a.sock
	frame, haveFrame := a.frames[msg.ID]
	a.mu.RUnlock()
	if sock == nil {
		return ErrNotOpen
	}
	if !haveFrame {
		frame = Frame{ID: msg.ID, DLC: msg.DLC}
	}
	data := frame.Data
	if err := msg.Encode(data[:], path, v); err != nil {
		return err
	}
	frame.Data = data
	if err := sock.WriteFrame(frame); err != nil {
		return fmt.Errorf("can: write frame for message %q: %w", msg.Name, err)
	}
	a.handleFrame(frame)
	return nil
}

// resolve looks up the DBC message and signal a (d, path) pair addresses.
func (a *Adapter) resolve(d api.Device, path string) (*Message, *Signal, error) {
	msgName := d.Labels[LabelMessage]
	if msgName == "" {
		return nil, nil, fmt.Errorf("%w: device %q", ErrMissingLabel, d.ID)
	}
	msg := a.db.MessageByName(msgName)
	if msg == nil {
		return nil, nil, fmt.Errorf("%w: %q (device %q)", ErrUnknownMessage, msgName, d.ID)
	}
	sig := msg.signal(path)
	if sig == nil {
		return nil, nil, fmt.Errorf("%w: %q in message %q (device %q)", ErrUnknownSignal, path, msgName, d.ID)
	}
	return msg, sig, nil
}

// readLoop drains sock until it errors (typically because Close closed the
// underlying fd) or a.done is closed.
func (a *Adapter) readLoop(sock rawSocket) {
	for {
		frame, err := sock.ReadFrame()
		if err != nil {
			select {
			case <-a.done:
				return
			default:
			}
			a.log.Warn("can: read frame", "err", err)
			return
		}
		a.handleFrame(frame)
	}
}

// handleFrame decodes frame (if it matches a known DBC message), updates
// the cache, and fans out to watchers of that message. Recovers from any
// panic during decoding so a single malformed/unexpected frame can't take
// down the read loop, let alone the gateway process (see package doc
// comment).
func (a *Adapter) handleFrame(frame Frame) {
	defer func() {
		if r := recover(); r != nil {
			a.log.Error("can: recovered from panic decoding frame", "id", frame.ID, "panic", r)
		}
	}()
	a.processFrame(frame)
}

// processFrame decodes frame via DecodeEach (not Decode — see its doc
// comment; this is the AC's benchmarked "decode latency < 1µs per frame"
// hot path, so it must not allocate a map per frame) and updates the
// cache. The second DecodeEach pass, fanning out to watchers, only runs
// when there's actually a callback and at least one watcher to reach —
// the common case (no one currently subscribed to this message) costs
// exactly one decode pass.
func (a *Adapter) processFrame(frame Frame) {
	msg := a.db.MessageByID(frame.ID)
	if msg == nil {
		return
	}

	a.mu.Lock()
	a.frames[msg.ID] = frame
	msg.DecodeEach(frame.Data[:], func(name string, v api.PropertyValue) {
		a.values[msg.Name+"/"+name] = v
	})
	var watchers []api.Device
	for _, d := range a.watched {
		if d.Labels[LabelMessage] == msg.Name {
			watchers = append(watchers, d)
		}
	}
	a.mu.Unlock()

	if a.onUpdate == nil || len(watchers) == 0 {
		return
	}
	msg.DecodeEach(frame.Data[:], func(name string, v api.PropertyValue) {
		for _, d := range watchers {
			a.onUpdate(d.ID, name, v)
		}
	})
}
