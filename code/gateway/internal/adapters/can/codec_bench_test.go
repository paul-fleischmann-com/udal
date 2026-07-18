package canadapter

import (
	"os"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

func benchDB(b *testing.B) *Database {
	b.Helper()
	f, err := os.Open("testdata/sample.dbc")
	if err != nil {
		b.Fatalf("open testdata: %v", err)
	}
	defer f.Close()
	db, err := ParseDBC(f)
	if err != nil {
		b.Fatalf("ParseDBC: %v", err)
	}
	return db
}

// BenchmarkMessageDecode_EngineData is F-11's "Decode latency < 1µs per
// frame (benchmark)" acceptance criterion, measured against DecodeEach —
// the allocation-free path Adapter's read loop actually calls per received
// frame (see processFrame) — not the map-returning Decode convenience
// wrapper. EngineData is a plain (non-multiplexed) two-signal message,
// representative of the common case.
// Run with: go test -bench=Decode -benchtime=1s ./internal/adapters/can/...
func BenchmarkMessageDecode_EngineData(b *testing.B) {
	msg := benchDB(b).MessageByName("EngineData")
	data := [8]byte{0x90, 0x01, 30, 0, 0, 0, 0, 0}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.DecodeEach(data[:], noopDecodeSink)
	}
}

// BenchmarkMessageDecode_Muxed covers the multiplexed-message path (extra
// work to resolve the selector before decoding), the more expensive of the
// two shapes.
func BenchmarkMessageDecode_Muxed(b *testing.B) {
	msg := benchDB(b).MessageByName("MuxedSensor")
	data := [8]byte{1, 0, 210, 4, 0, 0, 0, 0} // mux=1 -> SensorPressureB

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.DecodeEach(data[:], noopDecodeSink)
	}
}

func noopDecodeSink(string, api.PropertyValue) {}

// TestDecodeLatencyUnderOneMicrosecond enforces F-11's AC as an actual gate
// (not just a number printed by `go test -bench`, which nothing reads
// automatically) — go test itself fails if a regression pushes either
// message shape's decode latency over the 1µs/frame budget.
func TestDecodeLatencyUnderOneMicrosecond(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("skipping under -race: instrumentation inflates timing well past the 1µs budget")
	}
	const budget = 1000 * time.Nanosecond
	for _, bm := range []struct {
		name string
		fn   func(*testing.B)
	}{
		{"EngineData", BenchmarkMessageDecode_EngineData},
		{"MuxedSensor", BenchmarkMessageDecode_Muxed},
	} {
		result := testing.Benchmark(bm.fn)
		perOp := time.Duration(result.NsPerOp())
		t.Logf("%s: %s/op (%d allocs/op)", bm.name, perOp, result.AllocsPerOp())
		if perOp >= budget {
			t.Errorf("%s: decode latency %s/op, want < %s (F-11 AC)", bm.name, perOp, budget)
		}
	}
}
