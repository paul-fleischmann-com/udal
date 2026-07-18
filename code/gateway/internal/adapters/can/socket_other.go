//go:build !linux

package canadapter

// openSocket is a stub on non-Linux platforms: SocketCAN is a Linux kernel
// interface (req42.adoc TC-01: "macOS/Windows not supported for CAN in
// v1"). Kept buildable (not build-tag-excluded entirely) so the rest of the
// gateway still compiles and its non-CAN tests still run on a developer's
// Mac/Windows machine; Adapter.Open just fails clearly instead of the
// package not existing at all.
func openSocket(iface string) (rawSocket, error) {
	return nil, ErrLinuxOnly
}
