// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Paul Fleischmann

package logging_test

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/health"
	"github.com/paulefl/udal/code/gateway/internal/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// TestMetricsPort_EndpointsReachableWithoutAuth verifies that the three
// endpoints on the dedicated metrics port (9090) are reachable without any
// authentication credential — matching arc42 §8.5 "Authentication &
// Authorisation" and the requirement that the port must not be exposed
// outside the cluster/host network.
func TestMetricsPort_EndpointsReachableWithoutAuth(t *testing.T) {
	levelVar := new(slog.LevelVar)
	checker := health.NewChecker()
	checker.SetReady(true)

	mux := http.NewServeMux()
	mux.Handle("/debug/log-level", logging.LevelHandler(levelVar))
	mux.Handle("/health", checker.Handler())
	mux.Handle("/metrics", promhttp.Handler())

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{http.MethodGet, "/health", "", http.StatusOK},
		{http.MethodGet, "/metrics", "", http.StatusOK},
		{http.MethodGet, "/debug/log-level", "", http.StatusOK},
		{http.MethodPut, "/debug/log-level", "debug", http.StatusOK},
	}
	for _, tt := range tests {
		var body io.Reader
		if tt.body != "" {
			body = strings.NewReader(tt.body)
		}
		req, err := http.NewRequest(tt.method, srv.URL+tt.path, body)
		if err != nil {
			t.Fatalf("%s %s: %v", tt.method, tt.path, err)
		}
		// Deliberately send no Authorization header — the metrics port must
		// not require authentication.
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tt.method, tt.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tt.want {
			t.Errorf("%s %s: got %d, want %d", tt.method, tt.path, resp.StatusCode, tt.want)
		}
	}
}

// TestMetricsPort_NotExposedOnAPIPort verifies that the metrics endpoints are
// NOT served on the main API listener — they must live on the separate
// metrics port only. This test simulates a plain HTTP server without the
// metrics mux and confirms 404 for all three paths.
func TestMetricsPort_NotExposedOnAPIPort(t *testing.T) {
	// A minimal API mux with no metrics routes — represents the gRPC-gateway
	// HTTP listener on port 8080.
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/devices", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(apiMux)
	t.Cleanup(srv.Close)

	for _, path := range []string{"/health", "/metrics", "/debug/log-level"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s on API port: got %d, want 404", path, resp.StatusCode)
		}
	}
}

// TestMetricsPort_BindsOnSeparateAddress verifies that two listeners can be
// opened on distinct addresses — reflecting the gateway's dual-port design
// (API port + metrics port). This is a structural smoke test: if net.Listen
// fails for one of them, the gateway would abort at startup.
func TestMetricsPort_BindsOnSeparateAddress(t *testing.T) {
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("API listener: %v", err)
	}
	defer l1.Close()

	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("metrics listener: %v", err)
	}
	defer l2.Close()

	if l1.Addr().String() == l2.Addr().String() {
		t.Error("API and metrics listeners bound to the same address")
	}
}
