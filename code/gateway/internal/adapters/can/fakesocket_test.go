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
	written  []Frame
	closed   bool
	closeErr error
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{inbox: make(chan Frame, 16)}
}

// deliver injects a frame as if it had just arrived on the bus.
func (s *fakeSocket) deliver(f Frame) { s.inbox <- f }

func (s *fakeSocket) ReadFrame() (Frame, error) {
	f, ok := <-s.inbox
	if !ok {
		return Frame{}, errSocketClosed
	}
	return f, nil
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
