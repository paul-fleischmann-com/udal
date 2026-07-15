package api

import "sync"

// subscriberBuffer is the per-subscriber channel capacity. A slow subscriber
// that falls this far behind starts missing events rather than blocking
// Publish for everyone else — acceptable for v1, revisit if a backpressure
// story is needed.
const subscriberBuffer = 32

// Broker fans out PropertyUpdate events to Subscribe RPC handlers. It is
// safe for concurrent use.
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan PropertyUpdate]struct{} // key: deviceID
}

// NewBroker returns an empty Broker.
func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[chan PropertyUpdate]struct{})}
}

// Subscribe registers interest in updates for deviceID and returns a channel
// of events plus an unsubscribe function. Callers must call unsubscribe when
// done to release the channel.
func (b *Broker) Subscribe(deviceID string) (ch <-chan PropertyUpdate, unsubscribe func()) {
	c := make(chan PropertyUpdate, subscriberBuffer)

	b.mu.Lock()
	if b.subs[deviceID] == nil {
		b.subs[deviceID] = make(map[chan PropertyUpdate]struct{})
	}
	b.subs[deviceID][c] = struct{}{}
	b.mu.Unlock()

	return c, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs[deviceID], c)
		if len(b.subs[deviceID]) == 0 {
			delete(b.subs, deviceID)
		}
		close(c)
	}
}

// Publish fans out update to every current subscriber of update.DeviceID.
// Subscribers whose channel is full are skipped rather than blocking.
func (b *Broker) Publish(update PropertyUpdate) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.subs[update.DeviceID] {
		select {
		case c <- update:
		default:
		}
	}
}
