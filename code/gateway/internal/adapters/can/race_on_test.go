//go:build race

package canadapter

// raceDetectorEnabled is true when built with `go test -race`. The race
// detector instruments every memory access, inflating measured latency by
// several times — TestDecodeLatencyUnderOneMicrosecond skips under it
// rather than asserting a timing budget the instrumentation itself
// violates.
const raceDetectorEnabled = true
