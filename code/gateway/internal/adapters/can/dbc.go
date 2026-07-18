package canadapter

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Signal is one SG_ entry of a DBC message: a bit range within the
// message's payload, plus the affine transform (factor/offset) from raw
// integer to physical value (req42.adoc F-11: "Decodes frames to typed
// property values using DBC files").
type Signal struct {
	Name         string
	StartBit     int
	Length       int
	LittleEndian bool // @1 (Intel) if true, @0 (Motorola) if false
	Signed       bool
	Factor       float64
	Offset       float64
	Unit         string

	// IsMultiplexor marks the "M" selector signal of a multiplexed message
	// (F-11 AC: "Multiplexed signals correctly decoded (MUX support)").
	IsMultiplexor bool
	// MuxValue is non-nil for an "m<N>" signal: it's only present in a frame
	// when the message's multiplexor signal decodes to exactly *MuxValue.
	MuxValue *int

	// positions[i] is the array bit position (0 = LSB of payload byte 0 ...
	// 63 = MSB of byte 7) holding value bit i (weight 2^i) of this signal.
	// Precomputed once at parse time (see finalize) so Decode/Encode never
	// allocate or recompute it on the hot path (AC: decode latency < 1µs).
	positions []int
}

// Message is one BO_ entry of a DBC file: a CAN arbitration ID and the
// signals packed into its payload.
type Message struct {
	// ID is the message's arbitration ID exactly as written in the DBC
	// file's BO_ line, including the 0x80000000 bit Vector's format uses to
	// mark an extended (29-bit) ID — this is also SocketCAN's own can_id
	// wire convention, so it's used directly, unmodified, as the lookup key
	// against incoming/outgoing Frame.ID (see frame.go).
	ID   uint32
	Name string
	DLC  uint8

	Signals []Signal
}

// Database is a parsed DBC file's messages, cached in memory at startup
// (F-11 AC: "DBC file loaded at startup; signal map cached in memory").
type Database struct {
	byID   map[uint32]*Message
	byName map[string]*Message
}

// MessageByID returns the message with the given arbitration ID (Frame.ID
// convention, see Message.ID), or nil if none matches.
func (d *Database) MessageByID(id uint32) *Message {
	return d.byID[id]
}

// MessageByName returns the message with the given DBC name (BO_ name), or
// nil if none matches.
func (d *Database) MessageByName(name string) *Message {
	return d.byName[name]
}

var (
	boLineRE = regexp.MustCompile(`^BO_\s+(\d+)\s+(\w+)\s*:\s*(\d+)\s+\S+`)
	sgLineRE = regexp.MustCompile(`^\s*SG_\s+(\w+)\s*(M|m\d+)?\s*:\s*(\d+)\|(\d+)@([01])([+-])\s*\(([^,]+),([^)]+)\)\s*\[[^|]*\|[^\]]*\]\s*"([^"]*)"`)
)

// ParseDBC parses the subset of the Vector DBC grammar issue #25 needs: BO_
// message definitions and their SG_ signals (including multiplexed "M"/
// "m<N>" signals). Every other DBC section (BU_, BS_, VAL_, CM_, attribute
// definitions, ...) is ignored — none of it is needed to decode/encode
// signal values.
func ParseDBC(r io.Reader) (*Database, error) {
	db := &Database{byID: make(map[uint32]*Message), byName: make(map[string]*Message)}
	var cur *Message

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if m := boLineRE.FindStringSubmatch(trimmed); m != nil {
			id, err := strconv.ParseUint(m[1], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("dbc line %d: invalid message id %q: %w", lineNo, m[1], err)
			}
			dlc, err := strconv.ParseUint(m[3], 10, 8)
			if err != nil {
				return nil, fmt.Errorf("dbc line %d: invalid DLC %q: %w", lineNo, m[3], err)
			}
			cur = &Message{ID: uint32(id), Name: m[2], DLC: uint8(dlc)}
			db.byID[cur.ID] = cur
			db.byName[cur.Name] = cur
			continue
		}

		if m := sgLineRE.FindStringSubmatch(strings.TrimRight(line, " \t")); m != nil {
			if cur == nil {
				return nil, fmt.Errorf("dbc line %d: SG_ line before any BO_ message", lineNo)
			}
			startBit, _ := strconv.Atoi(m[3])
			length, _ := strconv.Atoi(m[4])
			factor, err := strconv.ParseFloat(m[7], 64)
			if err != nil {
				return nil, fmt.Errorf("dbc line %d: invalid factor %q: %w", lineNo, m[7], err)
			}
			offset, err := strconv.ParseFloat(m[8], 64)
			if err != nil {
				return nil, fmt.Errorf("dbc line %d: invalid offset %q: %w", lineNo, m[8], err)
			}
			sig := Signal{
				Name:         m[1],
				StartBit:     startBit,
				Length:       length,
				LittleEndian: m[5] == "1",
				Signed:       m[6] == "-",
				Factor:       factor,
				Offset:       offset,
				Unit:         m[9],
			}
			switch mux := m[2]; {
			case mux == "M":
				sig.IsMultiplexor = true
			case strings.HasPrefix(mux, "m"):
				n, err := strconv.Atoi(mux[1:])
				if err != nil {
					return nil, fmt.Errorf("dbc line %d: invalid mux value %q: %w", lineNo, mux, err)
				}
				sig.MuxValue = &n
			}
			sig.positions = bitPositions(sig.StartBit, sig.Length, sig.LittleEndian)
			cur.Signals = append(cur.Signals, sig)
			continue
		}

		// A BO_ line always starts a message; any other unrecognized
		// top-level ("BU_:", "VAL_ ...", etc.) line simply ends the current
		// message's signal block if it isn't indented as a signal — no
		// action needed, cur just stops being appended to until the next
		// BO_.
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dbc: %w", err)
	}
	return db, nil
}
