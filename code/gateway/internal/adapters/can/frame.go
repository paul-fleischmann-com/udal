package canadapter

import "encoding/binary"

// canFrameSize is sizeof(struct can_frame) in the Linux SocketCAN ABI:
// 4 bytes can_id + 1 byte can_dlc + 3 bytes padding + 8 bytes data.
const canFrameSize = 16

// canEFFFlag/canRTRFlag/canERRFlag are the top three bits of a SocketCAN
// can_id field (linux/can.h). Frame.ID keeps all three as-is on read (see
// unmarshalFrame) — the EFF bit matters (see Message.ID's doc comment,
// it's also Vector DBC's own extended-ID convention), and RTR/ERR frames
// are left for Database lookups to naturally miss on (see unmarshalFrame)
// rather than stripped here.
const (
	canEFFFlag = 0x80000000
	canRTRFlag = 0x40000000
	canERRFlag = 0x20000000
)

// Frame is one raw CAN frame, independent of the SocketCAN transport so the
// codec and Adapter logic can be unit tested without a real socket.
type Frame struct {
	// ID is the frame's arbitration ID exactly as it appears on the
	// SocketCAN wire's can_id field, EFF flag (0x80000000) included when
	// present — matching Message.ID's convention (see dbc.go) so a Frame's
	// ID can be looked up in a Database directly, with no
	// masking/reinterpretation at the boundary.
	ID   uint32
	DLC  uint8
	Data [8]byte
}

// marshalFrame encodes f into the 16-byte Linux struct can_frame wire
// format.
func marshalFrame(f Frame) []byte {
	buf := make([]byte, canFrameSize)
	binary.NativeEndian.PutUint32(buf[0:4], f.ID)
	buf[4] = f.DLC
	copy(buf[8:16], f.Data[:])
	return buf
}

// unmarshalFrame decodes buf (must be canFrameSize bytes, as returned by a
// SocketCAN raw socket read) into a Frame. RTR/error frames are kept as
// regular frames (ID retains its RTR/ERR bits so callers can filter if they
// need to) — the adapter's read loop drops them by checking against known
// DBC message IDs, which never match a flagged ID.
func unmarshalFrame(buf []byte) Frame {
	var f Frame
	f.ID = binary.NativeEndian.Uint32(buf[0:4])
	f.DLC = buf[4]
	copy(f.Data[:], buf[8:16])
	return f
}
