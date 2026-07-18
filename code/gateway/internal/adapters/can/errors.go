package canadapter

import "errors"

// Sentinel errors returned by the CAN adapter's DBC lookup and adapter
// operations (issue #25). Wrapped with fmt.Errorf("...: %w", ...) at the
// point of failure so callers can errors.Is against these while a human-
// readable message still names the specific message/signal/device involved.
var (
	// ErrUnknownMessage means a device's can.message label (see LabelMessage)
	// doesn't name any BO_ message in the loaded DBC file.
	ErrUnknownMessage = errors.New("can: unknown DBC message")

	// ErrUnknownSignal means a property_path doesn't name any SG_ signal
	// within the device's resolved DBC message.
	ErrUnknownSignal = errors.New("can: unknown DBC signal")

	// ErrMissingLabel means a device has transport=can but no can.message
	// label (see LabelMessage) — there's no way to resolve it to a DBC
	// message.
	ErrMissingLabel = errors.New("can: device has no can.message label")

	// ErrNoFrameYet means ReadProperty was called for a signal whose message
	// hasn't been observed on the bus since the adapter started (F-11 AC:
	// "returns decoded signal value from last received CAN frame" — there is
	// none yet).
	ErrNoFrameYet = errors.New("can: no frame received yet for this message")

	// ErrNotOpen means ReadProperty/WriteProperty was called before Open
	// successfully attached to a SocketCAN interface.
	ErrNotOpen = errors.New("can: adapter not open")

	// ErrLinuxOnly is returned by Open on platforms without SocketCAN
	// support (req42.adoc TC-01: "Linux >= 5.10 required; macOS/Windows not
	// supported for CAN in v1").
	ErrLinuxOnly = errors.New("can: SocketCAN is only supported on Linux (TC-01)")
)
