package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestCollectors_RegisteredWithExpectedNames covers F-22's AC "All four
// metrics present after startup" — gathering from the default registry
// (the one promhttp.Handler in cmd/gateway/main.go serves) must include
// all four, under exactly the names req42.adoc specifies.
func TestCollectors_RegisteredWithExpectedNames(t *testing.T) {
	// Touch every collector at least once so a fresh Gauge/CounterVec
	// (which produce zero samples until observed) shows up in Gather —
	// promauto registers the collector itself either way, but this test
	// wants to see the metric family, not just prove registration.
	DevicesOnline.Set(0)
	Requests.WithLabelValues("noop", "OK").Add(0)
	RequestDuration.WithLabelValues("noop").Observe(0)
	AdapterErrors.WithLabelValues("noop").Add(0)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	names := make(map[string]bool, len(families))
	for _, f := range families {
		names[f.GetName()] = true
	}

	for _, want := range []string{
		"udal_devices_online",
		"udal_requests_total",
		"udal_request_duration_seconds",
		"udal_adapter_errors_total",
	} {
		if !names[want] {
			t.Errorf("metric %q not found in Gather output", want)
		}
	}
}
