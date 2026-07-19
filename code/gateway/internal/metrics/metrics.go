// Package metrics defines the gateway's Prometheus collectors (req42.adoc
// F-22, GitHub issue #27) and a gRPC interceptor that records two of them
// on every request. Collectors are package-level vars registered once via
// promauto against prometheus.DefaultRegisterer — the standard
// client_golang pattern (promhttp.Handler, used in cmd/gateway/main.go,
// serves DefaultRegisterer by default) — rather than threaded through a
// constructor, so every package that needs to record a metric (this one's
// Interceptor, heartbeat.Monitor via a callback, device_service.go) can
// just import and use them directly.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DevicesOnline is F-22's "udal_devices_online" gauge — incremented on a
// device's RegisterDevice/heartbeat-driven transition to online,
// decremented on heartbeat.Monitor.Sweep's timeout-driven transition to
// offline (see heartbeat.WithOnStatusChange, wired in cmd/gateway/main.go).
var DevicesOnline = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "udal_devices_online",
	Help: "Number of devices currently online.",
})

// Requests is F-22's "udal_requests_total{operation,status}" counter —
// incremented once per gRPC request (also covering REST, proxied through
// the same server) by Interceptor. "operation" is the gRPC method's short
// name (e.g. "GetProperty"); "status" is the gRPC status code's string
// name (e.g. "OK", "NotFound").
var Requests = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "udal_requests_total",
	Help: "Total number of gateway API requests, by operation and result status.",
}, []string{"operation", "status"})

// RequestDuration is F-22's "udal_request_duration_seconds{operation}"
// histogram, recorded by Interceptor alongside Requests.
var RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "udal_request_duration_seconds",
	Help:    "Gateway API request duration in seconds, by operation.",
	Buckets: prometheus.DefBuckets,
}, []string{"operation"})

// AdapterErrors is F-22's "udal_adapter_errors_total{adapter}" counter.
// device_service.go increments it at each transport adapter call site
// (GetProperty/SetProperty's mqtt/http/can branches) — the single place
// every adapter operation's outcome is already observed uniformly,
// regardless of which specific error the adapter returned.
var AdapterErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "udal_adapter_errors_total",
	Help: "Total number of transport adapter errors, by adapter.",
}, []string{"adapter"})
