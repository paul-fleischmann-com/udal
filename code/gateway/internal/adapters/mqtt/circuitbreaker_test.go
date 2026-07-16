package mqtt

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_OpensAfterConsecutiveFailures(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < circuitBreakerMaxErrors-1; i++ {
		if err := cb.allow(); err != nil {
			t.Fatalf("allow() before threshold reached: %v", err)
		}
		cb.recordFailure()
	}
	if err := cb.allow(); err != nil {
		t.Fatalf("allow() at threshold-1 failures: %v", err)
	}
	cb.recordFailure() // circuitBreakerMaxErrors-th consecutive failure

	if err := cb.allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("allow() after %d consecutive failures = %v, want ErrCircuitOpen", circuitBreakerMaxErrors, err)
	}
}

func TestCircuitBreaker_SuccessResetsCount(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < circuitBreakerMaxErrors-1; i++ {
		cb.recordFailure()
	}
	cb.recordSuccess()
	// Another near-threshold run of failures shouldn't open the breaker,
	// since the success above reset the consecutive count.
	for i := 0; i < circuitBreakerMaxErrors-1; i++ {
		cb.recordFailure()
	}
	if err := cb.allow(); err != nil {
		t.Fatalf("allow() after success reset count: %v", err)
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeoutThenCloses(t *testing.T) {
	now := time.Now()
	cb := newCircuitBreaker()
	cb.now = func() time.Time { return now }

	for i := 0; i < circuitBreakerMaxErrors; i++ {
		cb.recordFailure()
	}
	if err := cb.allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("allow() immediately after opening = %v, want ErrCircuitOpen", err)
	}

	now = now.Add(circuitBreakerOpenDuration - time.Millisecond)
	if err := cb.allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("allow() just before open duration elapses = %v, want ErrCircuitOpen", err)
	}

	now = now.Add(2 * time.Millisecond) // now >= openedAt + circuitBreakerOpenDuration
	if err := cb.allow(); err != nil {
		t.Fatalf("allow() as half-open probe: %v", err)
	}
	cb.recordSuccess()

	if err := cb.allow(); err != nil {
		t.Fatalf("allow() after half-open probe succeeded: %v", err)
	}
}

func TestCircuitBreaker_FailedHalfOpenProbeReopensImmediately(t *testing.T) {
	now := time.Now()
	cb := newCircuitBreaker()
	cb.now = func() time.Time { return now }

	for i := 0; i < circuitBreakerMaxErrors; i++ {
		cb.recordFailure()
	}
	now = now.Add(circuitBreakerOpenDuration)
	if err := cb.allow(); err != nil {
		t.Fatalf("allow() as half-open probe: %v", err)
	}
	cb.recordFailure() // probe failed

	if err := cb.allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("allow() right after a failed half-open probe = %v, want ErrCircuitOpen (single failure should re-open, not require another %d-failure run)", err, circuitBreakerMaxErrors)
	}

	now = now.Add(circuitBreakerOpenDuration)
	if err := cb.allow(); err != nil {
		t.Fatalf("allow() after the re-opened breaker's own timeout elapsed: %v", err)
	}
}
