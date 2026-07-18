package canadapter

import (
	"os"
	"testing"
)

func loadTestDB(t *testing.T) *Database {
	t.Helper()
	f, err := os.Open("testdata/sample.dbc")
	if err != nil {
		t.Fatalf("open testdata: %v", err)
	}
	defer func() { _ = f.Close() }()
	db, err := ParseDBC(f)
	if err != nil {
		t.Fatalf("ParseDBC: %v", err)
	}
	return db
}

func TestParseDBC_Messages(t *testing.T) {
	db := loadTestDB(t)

	tests := []struct {
		name     string
		id       uint32
		dlc      uint8
		nSignals int
	}{
		{"EngineData", 256, 8, 2},
		{"MuxedSensor", 512, 8, 3},
		{"MotorolaMsg", 768, 8, 1},
		{"ExtendedMsg", 2147484160, 4, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := db.MessageByName(tt.name)
			if msg == nil {
				t.Fatalf("MessageByName(%q) = nil", tt.name)
			}
			if msg.ID != tt.id {
				t.Errorf("ID = %d, want %d", msg.ID, tt.id)
			}
			if msg.DLC != tt.dlc {
				t.Errorf("DLC = %d, want %d", msg.DLC, tt.dlc)
			}
			if len(msg.Signals) != tt.nSignals {
				t.Errorf("len(Signals) = %d, want %d", len(msg.Signals), tt.nSignals)
			}
			if db.MessageByID(tt.id) != msg {
				t.Errorf("MessageByID(%d) did not return the same message", tt.id)
			}
		})
	}

	if db.MessageByName("DoesNotExist") != nil {
		t.Error("MessageByName for unknown name should return nil")
	}
	if db.MessageByID(999999) != nil {
		t.Error("MessageByID for unknown id should return nil")
	}
}

func TestParseDBC_SignalFields(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("EngineData")
	sig := msg.signal("EngineTemp")
	if sig == nil {
		t.Fatal("signal EngineTemp not found")
	}
	if sig.StartBit != 16 || sig.Length != 8 {
		t.Errorf("StartBit/Length = %d/%d, want 16/8", sig.StartBit, sig.Length)
	}
	if !sig.LittleEndian {
		t.Error("LittleEndian = false, want true (@1)")
	}
	if !sig.Signed {
		t.Error("Signed = false, want true (trailing -)")
	}
	if sig.Factor != 1 || sig.Offset != -40 {
		t.Errorf("Factor/Offset = %v/%v, want 1/-40", sig.Factor, sig.Offset)
	}
	if sig.Unit != "degC" {
		t.Errorf("Unit = %q, want degC", sig.Unit)
	}
}

func TestParseDBC_Mux(t *testing.T) {
	db := loadTestDB(t)
	msg := db.MessageByName("MuxedSensor")

	mux := msg.signal("SensorMux")
	if mux == nil || !mux.IsMultiplexor {
		t.Fatalf("SensorMux: IsMultiplexor = %v, want true", mux)
	}

	a := msg.signal("SensorTempA")
	if a == nil || a.MuxValue == nil || *a.MuxValue != 0 {
		t.Fatalf("SensorTempA.MuxValue = %v, want 0", a)
	}
	b := msg.signal("SensorPressureB")
	if b == nil || b.MuxValue == nil || *b.MuxValue != 1 {
		t.Fatalf("SensorPressureB.MuxValue = %v, want 1", b)
	}
}
