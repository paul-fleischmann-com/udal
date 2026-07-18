package canadapter

import (
	"errors"
	"time"
)

// rawSocket is the platform-specific SocketCAN transport Adapter reads
// frames from and writes frames to. A narrow interface (not the concrete
// socket type) so Adapter's read-loop/cache/WriteProperty logic can be unit
// tested with a fake, without a real CAN interface (vcan0 or otherwise) —
// mirroring how mqtt.Adapter tests against a fake transport (see
// adapter_faketransport_test.go).
//
// ReadFrame is expected to return errReadTimeout periodically rather than
// blocking indefinitely (see socket_linux.go's SO_RCVTIMEO) — Adapter's
// readLoop relies on that bounded blocking duration to notice Close
// promptly instead of depending on the unspecified behavior of closing a
// file descriptor a blocking read is parked on.
type rawSocket interface {
	ReadFrame() (Frame, error)
	WriteFrame(Frame) error
	Close() error
}

// readTimeout bounds how long a single ReadFrame call may block before
// returning errReadTimeout, giving Adapter.readLoop a periodic point to
// check whether Close was called.
const readTimeout = 500 * time.Millisecond

// errReadTimeout is returned by rawSocket.ReadFrame when no frame arrived
// within readTimeout — not a real error, just a scheduled wakeup.
var errReadTimeout = errors.New("can: read timeout")
