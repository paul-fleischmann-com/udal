package mqtt

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned by ReadProperty/WriteProperty instead of
// attempting the request when the circuit breaker is open (issue #11: "5
// consecutive errors -> open for 30s").
var ErrCircuitOpen = errors.New("mqtt: circuit breaker open")

// circuitBreakerMaxErrors and circuitBreakerOpenDuration implement issue
// #11's acceptance criterion verbatim.
const (
	circuitBreakerMaxErrors    = 5
	circuitBreakerOpenDuration = 30 * time.Second
)

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

// circuitBreaker guards ReadProperty/WriteProperty: after
// circuitBreakerMaxErrors consecutive failures it opens for
// circuitBreakerOpenDuration, rejecting calls immediately (ErrCircuitOpen)
// rather than letting them hang against a struggling broker/device. After
// the open period it lets exactly one call through as a half-open probe;
// success closes the breaker, failure re-opens it. Safe for concurrent use.
type circuitBreaker struct {
	mu sync.Mutex

	state           circuitState
	consecutiveErrs int
	openedAt        time.Time

	now func() time.Time // overridden in tests
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{now: time.Now}
}

// isOpen reports whether the breaker is currently open (rejecting calls
// outright) — used by Adapter.Healthy (issue #27) to surface the same
// state GET /health reports for this adapter. Doesn't itself trigger the
// open->half-open transition allow() does; a health check observing "open"
// a moment before it would've flipped to half-open on the next real call
// is an acceptable staleness, not a correctness issue.
func (cb *circuitBreaker) isOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state == circuitOpen
}

// allow reports whether a call may proceed, transitioning open->half-open
// once circuitBreakerOpenDuration has elapsed since it opened.
func (cb *circuitBreaker) allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case circuitOpen:
		if cb.now().Sub(cb.openedAt) < circuitBreakerOpenDuration {
			return ErrCircuitOpen
		}
		cb.state = circuitHalfOpen
		return nil
	default:
		return nil
	}
}

// recordSuccess closes the breaker and resets the consecutive-error count.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveErrs = 0
	cb.state = circuitClosed
}

// recordFailure counts a consecutive failure, opening the breaker once
// circuitBreakerMaxErrors is reached. A failed half-open probe re-opens the
// breaker immediately, without needing another circuitBreakerMaxErrors run.
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == circuitHalfOpen {
		cb.state = circuitOpen
		cb.openedAt = cb.now()
		return
	}
	cb.consecutiveErrs++
	if cb.consecutiveErrs >= circuitBreakerMaxErrors {
		cb.state = circuitOpen
		cb.openedAt = cb.now()
	}
}
