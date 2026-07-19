package canadapter

import (
	"errors"
	"sync"
)

var errSocketClosed = errors.New("fake can socket closed")

// fakeSocket is a rawSocket that never talks to a real interface — it lets
// tests feed frames into Adapter's read loop (via inbox) and inspect
// frames Adapter writes (via WriteFrame), mirroring how
// adapter_faketransport_test.go tests the mqtt adapter without a broker.
type fakeSocket struct {
	mu       sync.Mutex
	inbox    chan Frame
	errCh    chan error
	written  []Frame
	closed   bool
	closeErr error
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{inbox: make(chan Frame, 16), errCh: make(chan error, 1)}
}

// deliver injects a frame as if it had just arrived on the bus.
func (s *fakeSocket) deliver(f Frame) { s.inbox <- f }

// failWith makes the next (or a currently-blocked) ReadFrame call return
// err — simulating a real socket error, as opposed to Close's expected
// shutdown (errSocketClosed).
func (s *fakeSocket) failWith(err error) { s.errCh <- err }

// ReadFrame has no ordering guarantee between inbox and errCh when both
// have something waiting at the same time — Go's select picks
// pseudo-randomly. No current test calls both deliver and failWith on the
// same fakeSocket without waiting for the delivered frame to be consumed
// first, so this doesn't affect anything today; a future test that queues
// a frame and then injects an error before the read loop drains the frame
// could flake. Keep deliver/failWith calls on one fakeSocket sequenced
// (wait for the frame's effect to be observable) rather than racing them.
func (s *fakeSocket) ReadFrame() (Frame, error) {
	select {
	case f, ok := <-s.inbox:
		if !ok {
			return Frame{}, errSocketClosed
		}
		return f, nil
	case err := <-s.errCh:
		return Frame{}, err
	}
}

func (s *fakeSocket) WriteFrame(f Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, f)
	return nil
}

func (s *fakeSocket) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.inbox)
	}
	return s.closeErr
}

func (s *fakeSocket) writtenFrames() []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Frame(nil), s.written...)
}
