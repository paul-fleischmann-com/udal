//go:build linux

package canadapter

import (
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// linuxSocket is rawSocket's real implementation: a SocketCAN raw socket
// (AF_CAN/SOCK_RAW/CAN_RAW, see linux/can.txt) bound to one interface
// (req42.adoc TC-01: Linux >= 5.10 required — SocketCAN itself has existed
// since well before that; TC-01 just states the floor this project
// verifies against).
type linuxSocket struct {
	fd int
}

// openSocket opens and binds a SocketCAN raw socket to iface (e.g. "can0",
// "vcan0").
func openSocket(iface string) (rawSocket, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("can: interface %q: %w", iface, err)
	}
	fd, err := unix.Socket(unix.AF_CAN, unix.SOCK_RAW, unix.CAN_RAW)
	if err != nil {
		return nil, fmt.Errorf("can: open raw socket: %w", err)
	}
	addr := &unix.SockaddrCAN{Ifindex: ifi.Index}
	if err := unix.Bind(fd, addr); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("can: bind to %q: %w", iface, err)
	}
	// Bounds how long a single blocking Read can take (see rawSocket's doc
	// comment in socket.go) — without this, a Read in flight when Close
	// closes the fd from another goroutine relies on unspecified OS
	// behavior to unblock; with it, the read loop simply retries within
	// readTimeout and notices Close on its own.
	tv := unix.NsecToTimeval(readTimeout.Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("can: set read timeout on %q: %w", iface, err)
	}
	return &linuxSocket{fd: fd}, nil
}

func (s *linuxSocket) ReadFrame() (Frame, error) {
	buf := make([]byte, canFrameSize)
	n, err := unix.Read(s.fd, buf)
	if err != nil {
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return Frame{}, errReadTimeout
		}
		return Frame{}, err
	}
	if n != canFrameSize {
		return Frame{}, fmt.Errorf("can: short read: %d bytes, want %d", n, canFrameSize)
	}
	return unmarshalFrame(buf), nil
}

func (s *linuxSocket) WriteFrame(f Frame) error {
	buf := marshalFrame(f)
	n, err := unix.Write(s.fd, buf)
	if err != nil {
		return err
	}
	if n != canFrameSize {
		return fmt.Errorf("can: short write: %d bytes, want %d", n, canFrameSize)
	}
	return nil
}

func (s *linuxSocket) Close() error {
	return unix.Close(s.fd)
}
