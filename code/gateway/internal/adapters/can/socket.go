package canadapter

// rawSocket is the platform-specific SocketCAN transport Adapter reads
// frames from and writes frames to. A narrow interface (not the concrete
// socket type) so Adapter's read-loop/cache/WriteProperty logic can be unit
// tested with a fake, without a real CAN interface (vcan0 or otherwise) —
// mirroring how mqtt.Adapter tests against a fake transport (see
// adapter_faketransport_test.go).
type rawSocket interface {
	ReadFrame() (Frame, error)
	WriteFrame(Frame) error
	Close() error
}
