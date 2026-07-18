//go:build loadtest

package mqtt

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// Defaults are CI-practical (finish in well under a minute) rather than the
// literal AC values (1,000 devices / 10s publish interval / 30min); every
// knob is overridable via env vars so the same harness also serves as the
// real, AC-matching soak-test tool (see plan doc for the actual soak run's
// documented results). simulatorConns is deliberately much smaller than
// deviceCount: the AC's heap/CPU/goroutine thresholds are about the
// gateway/adapter, not the load generator, and from the adapter's single
// subscribing connection, 20 real connections round-robin-publishing on
// behalf of 1,000 device IDs produce identical incoming traffic (1,000
// distinct topic strings) to 1,000 separate simulator connections would --
// only the simulator's own footprint differs, which isn't what's measured.
const (
	defaultDeviceCount     = 1000
	defaultPublishInterval = 10 * time.Second
	defaultDuration        = 20 * time.Second
	simulatorConns         = 20
	goroutineLeakTolerance = 50 // absolute slack for natural GC/runtime fluctuation
)

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

// cpuTime returns this process's total CPU time (user + system), for
// computing CPU% usage over a measured wall-clock window.
func cpuTime(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatalf("Getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// TestLoadSoak_1000Devices exercises issue #43 (QR-02): simulates
// deviceCount MQTT devices each publishing every publishInterval for
// duration, through one real Adapter (gateway side) and real Broker
// fan-out, then checks resource usage (heap, CPU, goroutines) against the
// acceptance criteria's thresholds.
//
// Run the AC-matching configuration explicitly, e.g.:
//
//	UDAL_TEST_MQTT_BROKER=tcp://127.0.0.1:1883 \
//	UDAL_LOADTEST_DEVICES=1000 UDAL_LOADTEST_PUBLISH_INTERVAL=10s UDAL_LOADTEST_DURATION=30m \
//	go test -tags loadtest -timeout 35m -run TestLoadSoak_1000Devices -v ./internal/adapters/mqtt/...
func TestLoadSoak_1000Devices(t *testing.T) {
	brokerURL := os.Getenv("UDAL_TEST_MQTT_BROKER")
	if brokerURL == "" {
		t.Skip("UDAL_TEST_MQTT_BROKER not set")
	}

	deviceCount := envInt("UDAL_LOADTEST_DEVICES", defaultDeviceCount)
	publishInterval := envDuration("UDAL_LOADTEST_PUBLISH_INTERVAL", defaultPublishInterval)
	duration := envDuration("UDAL_LOADTEST_DURATION", defaultDuration)
	t.Logf("config: devices=%d publishInterval=%s duration=%s simulatorConns=%d", deviceCount, publishInterval, duration, simulatorConns)

	deviceIDs := make([]string, deviceCount)
	for i := range deviceIDs {
		deviceIDs[i] = fmt.Sprintf("loadtest-dev-%05d", i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ─── Gateway side: one real Adapter, real Broker fan-out ────────────
	broker := api.NewBroker()
	a := New(brokerURL, func(deviceID, path string, v api.PropertyValue) {
		broker.Publish(api.PropertyUpdate{DeviceID: deviceID, PropertyPath: path, Value: v, Timestamp: time.Now()})
	})
	if err := a.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	for _, id := range deviceIDs {
		if err := a.WatchDevice(ctx, id); err != nil {
			t.Fatalf("WatchDevice(%s): %v", id, err)
		}
	}

	counts := make([]int64, deviceCount)
	subChans := make([]<-chan api.PropertyUpdate, deviceCount)
	unsubs := make([]func(), deviceCount)
	var subWG sync.WaitGroup
	for i, id := range deviceIDs {
		ch, unsubscribe := broker.Subscribe(id)
		subChans[i] = ch
		unsubs[i] = unsubscribe
		subWG.Add(1)
		go func(i int, ch <-chan api.PropertyUpdate) {
			defer subWG.Done()
			for range ch {
				atomic.AddInt64(&counts[i], 1)
			}
		}(i, ch)
	}

	// ─── Simulator: small pool of real MQTT connections, round-robin
	// publishing on behalf of all deviceCount device IDs ─────────────────
	sims := make([]transport, simulatorConns)
	for i := range sims {
		tr, err := connectV3(ctx, brokerURL, func(string, []byte) {})
		if err != nil {
			t.Fatalf("simulator connectV3 #%d: %v", i, err)
		}
		sims[i] = tr
	}

	runtime.GC()
	time.Sleep(200 * time.Millisecond) // let connection-setup goroutines settle before baselining
	goroutinesBefore := runtime.NumGoroutine()
	cpuBefore := cpuTime(t)
	wallStart := time.Now()

	simCtx, simCancel := context.WithCancel(ctx)
	var simWG sync.WaitGroup
	for i, id := range deviceIDs {
		conn := sims[i%len(sims)]
		topic := topicProps(id, "temperature")
		simWG.Add(1)
		go func(conn transport, topic string) {
			defer simWG.Done()
			ticker := time.NewTicker(publishInterval)
			defer ticker.Stop()
			for {
				select {
				case <-simCtx.Done():
					return
				case <-ticker.C:
					_ = conn.Publish(simCtx, topic, []byte(`{"float":21.5}`))
				}
			}
		}(conn, topic)
	}

	// Sample goroutine counts through the steady-state run: a genuine leak
	// (growing per publish cycle, not just a one-time warmup bump) shows up
	// as the last sample being well above the first.
	var goroutineSamples []int
	sampleEvery := duration / 5
	if sampleEvery < time.Second {
		sampleEvery = time.Second
	}
	sampleCtx, stopSampling := context.WithCancel(simCtx)
	var sampleWG sync.WaitGroup
	sampleWG.Add(1)
	go func() {
		defer sampleWG.Done()
		ticker := time.NewTicker(sampleEvery)
		defer ticker.Stop()
		for {
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
				goroutineSamples = append(goroutineSamples, runtime.NumGoroutine())
			}
		}
	}()

	time.Sleep(duration)
	simCancel()
	simWG.Wait()
	stopSampling()
	sampleWG.Wait()
	time.Sleep(500 * time.Millisecond) // let the last in-flight publishes land

	cpuAfter := cpuTime(t)
	wallElapsed := time.Since(wallStart)

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// ─── Assertions: all devices heard from ──────────────────────────────
	zeroCount := 0
	for i, c := range counts {
		if atomic.LoadInt64(&c) == 0 {
			zeroCount++
			if zeroCount <= 5 { // don't flood the log for a systemic failure
				t.Logf("device %s received zero updates", deviceIDs[i])
			}
		}
	}
	if zeroCount > 0 {
		t.Errorf("%d/%d devices received zero updates", zeroCount, deviceCount)
	}

	// ─── Assertions: heap ─────────────────────────────────────────────────
	heapMB := float64(memStats.HeapAlloc) / (1024 * 1024)
	t.Logf("heap alloc: %.1f MB", heapMB)
	if heapMB > 500 {
		t.Errorf("heap alloc = %.1f MB, want < 500 MB", heapMB)
	}

	// ─── Assertions: CPU ──────────────────────────────────────────────────
	cpuPercent := 100 * (cpuAfter - cpuBefore).Seconds() / wallElapsed.Seconds() / float64(runtime.NumCPU())
	t.Logf("CPU usage: %.1f%% (over %s wall, %d CPUs)", cpuPercent, wallElapsed, runtime.NumCPU())
	if cpuPercent > 70 {
		t.Errorf("CPU usage = %.1f%%, want < 70%%", cpuPercent)
	}

	// ─── Assertions: goroutine stability during the run ──────────────────
	t.Logf("goroutine samples during run: %v (baseline before run: %d)", goroutineSamples, goroutinesBefore)
	if len(goroutineSamples) >= 2 {
		first, last := goroutineSamples[0], goroutineSamples[len(goroutineSamples)-1]
		if last > first+goroutineLeakTolerance {
			t.Errorf("goroutine count grew from %d to %d during the run (tolerance %d) -- possible leak", first, last, goroutineLeakTolerance)
		}
	}

	// pprof heap profile, per the AC's "verified via pprof".
	profilePath := fmt.Sprintf("%s/loadtest-heap-%d.pprof", os.TempDir(), time.Now().Unix())
	if f, err := os.Create(profilePath); err == nil {
		if err := pprof.WriteHeapProfile(f); err != nil {
			t.Logf("write heap profile: %v", err)
		}
		_ = f.Close()
		t.Logf("heap profile written to %s", profilePath)
	} else {
		t.Logf("create heap profile file: %v", err)
	}

	// ─── Teardown, then confirm goroutines return near the pre-test baseline ───
	for _, unsubscribe := range unsubs {
		unsubscribe()
	}
	subWG.Wait()
	for _, tr := range sims {
		_ = tr.Disconnect(ctx)
	}
	if err := a.Disconnect(ctx); err != nil {
		t.Logf("adapter disconnect: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	goroutinesAfterTeardown := runtime.NumGoroutine()
	t.Logf("goroutines after full teardown: %d (baseline before run: %d)", goroutinesAfterTeardown, goroutinesBefore)
	if goroutinesAfterTeardown > goroutinesBefore+goroutineLeakTolerance {
		t.Errorf("goroutine count after teardown = %d, want <= baseline %d + tolerance %d -- possible leak surviving cleanup",
			goroutinesAfterTeardown, goroutinesBefore, goroutineLeakTolerance)
	}
}
