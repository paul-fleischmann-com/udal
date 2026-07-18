package canadapter

import (
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

func floatVal(t *testing.T, v api.PropertyValue) float64 {
	t.Helper()
	if v.FloatVal == nil {
		t.Fatalf("value %+v has no FloatVal", v)
	}
	return *v.FloatVal
}

func intVal(t *testing.T, v api.PropertyValue) int64 {
	t.Helper()
	if v.IntVal == nil {
		t.Fatalf("value %+v has no IntVal", v)
	}
	return *v.IntVal
}

// TestDecode_ScaledLittleEndianUnsigned hand-verifies EngineSpeed (start=0,
// len=16, @1 LE, unsigned, factor 0.25): raw 400 (0x0190, byte0=0x90,
// byte1=0x01) decodes to 400*0.25 = 100.0.
func TestDecode_ScaledLittleEndianUnsigned(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("EngineData")
	data := [8]byte{0x90, 0x01, 0, 0, 0, 0, 0, 0}
	got := msg.Decode(data[:])
	if v := floatVal(t, got["EngineSpeed"]); v != 100.0 {
		t.Errorf("EngineSpeed = %v, want 100.0", v)
	}
}

// TestSignal_EncodeDecodeRoundtrip covers a signed, negatively-offset
// signal (EngineTemp: factor 1, offset -40) across both a positive and a
// negative raw two's-complement encoding.
func TestSignal_EncodeDecodeRoundtrip(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("EngineData")

	tests := []struct {
		name     string
		physical float64
	}{
		{"positive raw", -10.0},
		{"negative raw", -45.0},
		{"zero raw", -40.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data [8]byte
			if err := msg.Encode(data[:], "EngineTemp", api.FloatValue(tt.physical)); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got := msg.Decode(data[:])
			if v := floatVal(t, got["EngineTemp"]); v != tt.physical {
				t.Errorf("roundtrip = %v, want %v (data=%x)", v, tt.physical, data)
			}
		})
	}
}

// TestDecode_UnscaledSignalIsInt covers the factor==1/offset==0 special
// case (see Signal.decode): SensorMux should decode to IntVal, not FloatVal.
func TestDecode_UnscaledSignalIsInt(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("MuxedSensor")
	data := [8]byte{2, 0, 0, 0, 0, 0, 0, 0}
	got := msg.Decode(data[:])
	v, ok := got["SensorMux"]
	if !ok {
		t.Fatal("SensorMux missing from decode result")
	}
	if v.IntVal == nil || v.FloatVal != nil {
		t.Errorf("SensorMux = %+v, want IntVal set, FloatVal nil", v)
	}
	if intVal(t, v) != 2 {
		t.Errorf("SensorMux = %d, want 2", *v.IntVal)
	}
}

// TestDecode_Mux verifies multiplexed signal selection: SensorTempA only
// appears when SensorMux==0, SensorPressureB only when SensorMux==1 (F-11
// AC: "Multiplexed signals correctly decoded").
func TestDecode_Mux(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("MuxedSensor")

	t.Run("mux=0 selects SensorTempA", func(t *testing.T) {
		var data [8]byte
		if err := msg.Encode(data[:], "SensorTempA", api.FloatValue(60.0)); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		got := msg.Decode(data[:])
		if _, ok := got["SensorTempA"]; !ok {
			t.Error("SensorTempA missing, want present when mux=0")
		}
		if _, ok := got["SensorPressureB"]; ok {
			t.Error("SensorPressureB present, want absent when mux=0")
		}
		if intVal(t, got["SensorMux"]) != 0 {
			t.Errorf("SensorMux = %d, want 0 (auto-set by Encode)", *got["SensorMux"].IntVal)
		}
		if v := floatVal(t, got["SensorTempA"]); v != 60.0 {
			t.Errorf("SensorTempA = %v, want 60.0", v)
		}
	})

	t.Run("mux=1 selects SensorPressureB", func(t *testing.T) {
		var data [8]byte
		if err := msg.Encode(data[:], "SensorPressureB", api.FloatValue(123.4)); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		got := msg.Decode(data[:])
		if _, ok := got["SensorPressureB"]; !ok {
			t.Error("SensorPressureB missing, want present when mux=1")
		}
		if _, ok := got["SensorTempA"]; ok {
			t.Error("SensorTempA present, want absent when mux=1")
		}
		if intVal(t, got["SensorMux"]) != 1 {
			t.Errorf("SensorMux = %d, want 1 (auto-set by Encode)", *got["SensorMux"].IntVal)
		}
		if v := floatVal(t, got["SensorPressureB"]); v-123.4 > 1e-9 || 123.4-v > 1e-9 {
			t.Errorf("SensorPressureB = %v, want ~123.4", v)
		}
	})
}

// TestDecode_Motorola hand-verifies BEValue (start=7, len=12, @0 big-endian,
// unsigned): the 12-bit value spans byte0 in full (its high 8 bits) plus
// byte1's top nibble (its low 4 bits) — see bitPositions' doc comment.
// byte0=0xAB, byte1's top nibble=0xC gives value 0xABC == 2748.
func TestDecode_Motorola(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("MotorolaMsg")
	data := [8]byte{0xAB, 0xC0, 0, 0, 0, 0, 0, 0}
	got := msg.Decode(data[:])
	if v := intVal(t, got["BEValue"]); v != 2748 {
		t.Errorf("BEValue = %d, want 2748 (0xABC)", v)
	}
}

// TestEncode_MotorolaRoundtrip encodes into a zeroed frame and checks the
// untouched low nibble of byte1 stays zero (packRaw must not touch bits
// outside the signal's own positions).
func TestEncode_MotorolaRoundtrip(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("MotorolaMsg")
	var data [8]byte
	if err := msg.Encode(data[:], "BEValue", api.IntValue(2748)); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if data[0] != 0xAB {
		t.Errorf("data[0] = %#x, want 0xab", data[0])
	}
	if data[1] != 0xC0 {
		t.Errorf("data[1] = %#x, want 0xc0 (low nibble untouched)", data[1])
	}
	got := msg.Decode(data[:])
	if v := intVal(t, got["BEValue"]); v != 2748 {
		t.Errorf("roundtrip BEValue = %d, want 2748", v)
	}
}

func TestExtendedMessage_IDIncludesEFFBit(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("ExtendedMsg")
	if msg.ID&canEFFFlag == 0 {
		t.Errorf("ExtendedMsg.ID = %#x, want EFF bit (0x80000000) set", msg.ID)
	}
	if db.MessageByID(msg.ID) != msg {
		t.Error("MessageByID with the EFF-flagged ID should resolve the same message")
	}
}

func TestMessage_Encode_UnknownSignal(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("EngineData")
	var data [8]byte
	err := msg.Encode(data[:], "NoSuchSignal", api.IntValue(1))
	if err == nil {
		t.Fatal("Encode with unknown signal name: want error, got nil")
	}
}

func TestSignal_Encode_UnsupportedValueType(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("EngineData")
	var data [8]byte
	err := msg.Encode(data[:], "EngineSpeed", api.StringValue("nope"))
	if err == nil {
		t.Fatal("Encode with a string PropertyValue: want error, got nil")
	}
}
