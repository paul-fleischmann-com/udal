package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

// ErrDeviceNotConnected is returned by CommandRouter.Dispatch when no
// StreamCommands channel is currently registered for the target device —
// e.g. because it's not a directly-connected gRPC device, or its stream has
// disconnected.
var ErrDeviceNotConnected = errors.New("device not connected via StreamCommands")

// Command is a command dispatched to a directly-connected device.
type Command struct {
	ID     string
	Name   string
	Params map[string]any
}

// CommandResult is a device's response to one Command, correlated by ID.
type CommandResult struct {
	ID      string
	Success bool
	Error   string // populated iff !Success
	Result  any
}

// NewCommandID returns a random identifier for correlating a Command with
// its CommandResult.
func NewCommandID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// CommandRouter pairs SendCommand callers with a device's StreamCommands
// channel, correlating requests and responses by Command.ID so multiple
// commands can be in flight concurrently for the same device. Safe for
// concurrent use.
type CommandRouter struct {
	mu       sync.Mutex
	channels map[string]chan Command       // deviceID -> commands awaiting delivery
	pending  map[string]chan CommandResult // command ID -> where to deliver its result
}

// NewCommandRouter returns an empty CommandRouter.
func NewCommandRouter() *CommandRouter {
	return &CommandRouter{
		channels: make(map[string]chan Command),
		pending:  make(map[string]chan CommandResult),
	}
}

// commandBuffer is the outgoing-command channel capacity per device — same
// reasoning as Broker's subscriberBuffer: bounded, so a stalled device can't
// block the router indefinitely.
const commandBuffer = 8

// Register opens a command channel for deviceID, for the StreamCommands
// handler to read from. Only one connection per device is supported; a
// second Register for the same device replaces the first (the prior one's
// eventual unregister will not affect the new registration).
func (r *CommandRouter) Register(deviceID string) (commands <-chan Command, unregister func()) {
	ch := make(chan Command, commandBuffer)
	r.mu.Lock()
	r.channels[deviceID] = ch
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if r.channels[deviceID] == ch {
			delete(r.channels, deviceID)
		}
		r.mu.Unlock()
		close(ch)
	}
}

// SubmitResult delivers a device's CommandResult to whichever Dispatch call
// is waiting for it. It's a no-op if nothing is waiting (e.g. Dispatch
// already timed out).
func (r *CommandRouter) SubmitResult(res CommandResult) {
	r.mu.Lock()
	ch, ok := r.pending[res.ID]
	if ok {
		delete(r.pending, res.ID)
	}
	r.mu.Unlock()
	if ok {
		ch <- res
	}
}

// Dispatch sends cmd to deviceID's command channel and waits for a
// CommandResult, honoring ctx's deadline/cancellation for both the send and
// the wait. Returns ErrDeviceNotConnected if no device is currently
// registered.
func (r *CommandRouter) Dispatch(ctx context.Context, deviceID string, cmd Command) (CommandResult, error) {
	r.mu.Lock()
	ch, ok := r.channels[deviceID]
	if !ok {
		r.mu.Unlock()
		return CommandResult{}, ErrDeviceNotConnected
	}
	resultCh := make(chan CommandResult, 1)
	r.pending[cmd.ID] = resultCh
	r.mu.Unlock()

	select {
	case ch <- cmd:
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.pending, cmd.ID)
		r.mu.Unlock()
		return CommandResult{}, ctx.Err()
	}

	select {
	case res := <-resultCh:
		return res, nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.pending, cmd.ID)
		r.mu.Unlock()
		return CommandResult{}, ctx.Err()
	}
}
