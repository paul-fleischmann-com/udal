package canadapter

import (
	"fmt"
	"math"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// bitPositions returns, for a signal occupying [startBit, startBit+length)
// in DBC bit numbering, the array bit position (0 = LSB of payload byte 0
// ... 63 = MSB of byte 7) of each value bit, indexed by significance (index
// i = the bit with weight 2^i).
//
// Both byte orders use the same numbering for startBit itself — position =
// 8*byteIndex + bitInByte, bitInByte 0 (LSB) .. 7 (MSB) — they differ only
// in which end of the signal startBit names and which direction subsequent
// bits are read:
//
//   - Intel (@1, littleEndian): startBit is the signal's LSB. Bits are
//     consecutive increasing positions (startBit, startBit+1, ...) —
//     increasing position = increasing significance.
//   - Motorola (@0, big-endian): startBit is the signal's MSB. Subsequent
//     (less significant) bits follow physical reading order: decreasing
//     bitInByte within a byte, then jumping to the MSB (bitInByte 7) of the
//     next byte once a byte's LSB (bitInByte 0) is reached.
func bitPositions(startBit, length int, littleEndian bool) []int {
	positions := make([]int, length)
	if littleEndian {
		for i := 0; i < length; i++ {
			positions[i] = startBit + i
		}
		return positions
	}
	pos := startBit
	for i := length - 1; i >= 0; i-- {
		positions[i] = pos
		if pos%8 == 0 {
			pos += 15
		} else {
			pos--
		}
	}
	return positions
}

// extractRaw reads the raw (unscaled, unsigned-pattern) integer value of a
// signal whose value bits sit at positions (see bitPositions) out of an
// 8-byte CAN payload. positions is precomputed once at parse time, so this
// does no allocation and no per-call bit-layout arithmetic beyond the shift
// itself (AC: decode latency < 1µs per frame).
func extractRaw(data []byte, positions []int) uint64 {
	var v uint64
	for i, pos := range positions {
		byteIdx := pos / 8
		if byteIdx >= len(data) {
			continue
		}
		bit := (data[byteIdx] >> uint(pos%8)) & 1
		v |= uint64(bit) << uint(i)
	}
	return v
}

// packRaw is extractRaw's inverse: it writes raw's bits into data at
// positions, leaving every other bit of data untouched (so encoding one
// signal never clobbers a sibling signal sharing the same message payload —
// see Adapter.WriteProperty's read-modify-write).
func packRaw(data []byte, positions []int, raw uint64) {
	for i, pos := range positions {
		byteIdx := pos / 8
		if byteIdx >= len(data) {
			continue
		}
		bit := byte(1) << uint(pos%8)
		if (raw>>uint(i))&1 != 0 {
			data[byteIdx] |= bit
		} else {
			data[byteIdx] &^= bit
		}
	}
}

// rawToSigned sign-extends a length-bit two's-complement pattern read into
// the low bits of raw.
func rawToSigned(raw uint64, length int) int64 {
	if length >= 64 {
		return int64(raw)
	}
	signBit := uint64(1) << uint(length-1)
	if raw&signBit != 0 {
		return int64(raw) - int64(uint64(1)<<uint(length))
	}
	return int64(raw)
}

// decode reads sig's physical value out of an 8-byte CAN payload. A signal
// with Factor==1 and Offset==0 decodes to api.PropertyValue.IntVal
// (preserving exact integer precision); every other signal — the general
// DBC case, a scaled physical quantity — decodes to FloatVal.
func (sig *Signal) decode(data []byte) api.PropertyValue {
	raw := extractRaw(data, sig.positions)
	if sig.Factor == 1 && sig.Offset == 0 {
		if sig.Signed {
			return api.IntValue(rawToSigned(raw, sig.Length))
		}
		return api.IntValue(int64(raw))
	}
	var physical float64
	if sig.Signed {
		physical = float64(rawToSigned(raw, sig.Length))*sig.Factor + sig.Offset
	} else {
		physical = float64(raw)*sig.Factor + sig.Offset
	}
	return api.FloatValue(physical)
}

// encode is decode's inverse: it computes sig's raw length-bit pattern for
// physical value v and writes it into data at sig.positions (read-modify-
// write — see packRaw).
func (sig *Signal) encode(data []byte, v api.PropertyValue) error {
	var physical float64
	switch {
	case v.FloatVal != nil:
		physical = *v.FloatVal
	case v.IntVal != nil:
		physical = float64(*v.IntVal)
	case v.BoolVal != nil:
		if *v.BoolVal {
			physical = 1
		}
	default:
		return fmt.Errorf("can: signal %q: value has no numeric representation", sig.Name)
	}
	rawF := (physical - sig.Offset) / sig.Factor
	rawInt := int64(math.Round(rawF))
	mask := ^uint64(0)
	if sig.Length < 64 {
		mask = uint64(1)<<uint(sig.Length) - 1
	}
	packRaw(data, sig.positions, uint64(rawInt)&mask)
	return nil
}

// DecodeEach calls fn for every currently-active signal of msg out of an
// 8-byte CAN payload. A multiplexed ("m<N>") signal is visited only when
// the message's multiplexor ("M") signal decodes to that exact N (F-11 AC:
// "Multiplexed signals correctly decoded"). Unlike Decode, this allocates
// nothing beyond what fn itself does — it's the path Adapter's read loop
// uses (AC: decode latency < 1µs per frame; profiling showed the map
// allocation/GC-scan in Decode's map[string]... result was the actual cost,
// not the bit extraction itself).
func (msg *Message) DecodeEach(data []byte, fn func(signalName string, v api.PropertyValue)) {
	muxVal, haveMux := 0, false
	for i := range msg.Signals {
		if msg.Signals[i].IsMultiplexor {
			muxVal = int(extractRaw(data, msg.Signals[i].positions))
			haveMux = true
			break
		}
	}
	for i := range msg.Signals {
		sig := &msg.Signals[i]
		if sig.MuxValue != nil && (!haveMux || *sig.MuxValue != muxVal) {
			continue
		}
		fn(sig.Name, sig.decode(data))
	}
}

// Decode is DecodeEach collected into a map, keyed by signal name — a
// convenience for callers (mainly tests) that want the whole result at
// once rather than streaming it via a callback.
func (msg *Message) Decode(data []byte) map[string]api.PropertyValue {
	result := make(map[string]api.PropertyValue, len(msg.Signals))
	msg.DecodeEach(data, func(name string, v api.PropertyValue) { result[name] = v })
	return result
}

// signal returns msg's signal named name, or nil if there is none.
func (msg *Message) signal(name string) *Signal {
	for i := range msg.Signals {
		if msg.Signals[i].Name == name {
			return &msg.Signals[i]
		}
	}
	return nil
}

// Encode writes signalName's raw bit pattern for v into data (read-modify-
// write over whatever data already holds — see Adapter.WriteProperty). If
// signalName is a multiplexed ("m<N>") signal, its message's multiplexor
// signal is also set to N, so the frame stays self-consistent for any
// decoder reading it back.
func (msg *Message) Encode(data []byte, signalName string, v api.PropertyValue) error {
	sig := msg.signal(signalName)
	if sig == nil {
		return fmt.Errorf("%w: %q in message %q", ErrUnknownSignal, signalName, msg.Name)
	}
	if err := sig.encode(data, v); err != nil {
		return err
	}
	if sig.MuxValue != nil {
		for i := range msg.Signals {
			if msg.Signals[i].IsMultiplexor {
				packRaw(data, msg.Signals[i].positions, uint64(*sig.MuxValue))
				break
			}
		}
	}
	return nil
}
