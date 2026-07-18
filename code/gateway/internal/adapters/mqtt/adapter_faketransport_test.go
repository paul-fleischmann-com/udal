package mqtt

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

// respondingTransport is a fake transport whose Publish synchronously
// invokes onPublish -- called after the Adapter has already registered its
// waiter (ReadProperty/WriteProperty always register before publishing), so
// tests can simulate a device's reply deterministically without a real
// broker, a goroutine, or a sleep.
type respondingTransport struct {
	mu               sync.Mutex
	publishErr       error
	onPublish        func(topic string, payload []byte)
	subscribedTopics []string
}

func (f *respondingTransport) Publish(_ context.Context, topic string, payload []byte) error {
	f.mu.Lock()
	err := f.publishErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if f.onPublish != nil {
		f.onPublish(topic, payload)
	}
	return nil
}
func (f *respondingTransport) Subscribe(_ context.Context, topic string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribedTopics = append(f.subscribedTopics, topic)
	return nil
}
func (f *respondingTransport) Disconnect(context.Context) error { return nil }

func (f *respondingTransport) subscriptions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.subscribedTopics...)
}

func newTestAdapter(onUpdate OnPropertyUpdate, tr transport, opts ...Option) *Adapter {
	if onUpdate == nil {
		onUpdate = func(string, string, api.PropertyValue) {}
	}
	a := New("mqtt://fake", onUpdate, opts...)
	a.tr = tr
	return a
}

func TestAdapter_ReadPropertyRoundTrip(t *testing.T) {
	ft := &respondingTransport{}
	a := newTestAdapter(nil, ft)
	ft.onPublish = func(topic string, _ []byte) {
		if strings.HasSuffix(topic, "/get") {
			a.dispatch(topicProps("dev-1", "temperature"), []byte(`{"float":21.5}`))
		}
	}

	v, err := a.ReadProperty(context.Background(), "dev-1", "temperature")
	if err != nil {
		t.Fatalf("ReadProperty: %v", err)
	}
	if v.FloatVal == nil || *v.FloatVal != 21.5 {
		t.Errorf("ReadProperty = %+v, want float 21.5", v)
	}
}

func TestAdapter_ReadPropertyAlsoFansOut(t *testing.T) {
	var got []string
	ft := &respondingTransport{}
	a := newTestAdapter(func(deviceID, path string, _ api.PropertyValue) {
		got = append(got, deviceID+"/"+path)
	}, ft)
	ft.onPublish = func(topic string, _ []byte) {
		if strings.HasSuffix(topic, "/get") {
			a.dispatch(topicProps("dev-1", "temperature"), []byte(`{"float":21.5}`))
		}
	}

	if _, err := a.ReadProperty(context.Background(), "dev-1", "temperature"); err != nil {
		t.Fatalf("ReadProperty: %v", err)
	}
	if len(got) != 1 || got[0] != "dev-1/temperature" {
		t.Errorf("fan-out updates = %v, want [dev-1/temperature]", got)
	}
}

func TestAdapter_WritePropertyRoundTrip(t *testing.T) {
	ft := &respondingTransport{}
	a := newTestAdapter(nil, ft)
	ft.onPublish = func(topic string, _ []byte) {
		if strings.HasSuffix(topic, "/set") {
			a.dispatch(topicSetAck("dev-1", "led"), []byte(`{}`))
		}
	}

	if err := a.WriteProperty(context.Background(), "dev-1", "led", api.BoolValue(true)); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
}

func TestAdapter_ReadPropertyTimeout(t *testing.T) {
	ft := &respondingTransport{} // never replies
	a := newTestAdapter(nil, ft, WithRequestTimeout(50*time.Millisecond))

	_, err := a.ReadProperty(context.Background(), "dev-1", "temperature")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReadProperty error = %v, want context.DeadlineExceeded", err)
	}
}

func TestAdapter_UnsolicitedDispatchFansOutWithoutAnyRequest(t *testing.T) {
	var got []string
	a := newTestAdapter(func(deviceID, path string, _ api.PropertyValue) {
		got = append(got, deviceID+"/"+path)
	}, &respondingTransport{})

	a.dispatch(topicProps("dev-1", "humidity"), []byte(`{"float":55}`))

	if len(got) != 1 || got[0] != "dev-1/humidity" {
		t.Errorf("fan-out updates = %v, want [dev-1/humidity]", got)
	}
}

func TestAdapter_WatchDeviceSubscribesToStatusTopic(t *testing.T) {
	ft := &respondingTransport{}
	a := newTestAdapter(nil, ft)

	if err := a.WatchDevice(context.Background(), "dev-1"); err != nil {
		t.Fatalf("WatchDevice: %v", err)
	}

	subs := ft.subscriptions()
	wantProps, wantStatus := false, false
	for _, s := range subs {
		if s == topicPropsWildcard("dev-1") {
			wantProps = true
		}
		if s == topicStatus("dev-1") {
			wantStatus = true
		}
	}
	if !wantProps || !wantStatus {
		t.Errorf("subscriptions = %v, want both %q and %q", subs, topicPropsWildcard("dev-1"), topicStatus("dev-1"))
	}
}

func TestAdapter_DispatchInvokesOnHeartbeatForStatusTopic(t *testing.T) {
	var touched []string
	a := newTestAdapter(nil, &respondingTransport{}, WithOnHeartbeat(func(deviceID string) {
		touched = append(touched, deviceID)
	}))

	a.dispatch(topicStatus("dev-1"), []byte("alive"))

	if len(touched) != 1 || touched[0] != "dev-1" {
		t.Errorf("touched = %v, want [dev-1]", touched)
	}
}

func TestAdapter_DispatchWithoutOnHeartbeatDoesNotPanic(t *testing.T) {
	a := newTestAdapter(nil, &respondingTransport{})
	a.dispatch(topicStatus("dev-1"), []byte("alive")) // must not panic with a nil onHeartbeat
}

func TestAdapter_RejectsDeviceIDContainingWildcardLevel(t *testing.T) {
	// A deviceID (or path) of exactly "+" would turn WatchDevice's
	// per-device wildcard "udal/{deviceId}/props/#" into "udal/+/props/#",
	// matching every device on the broker rather than just this one — must
	// be rejected, not silently subscribed.
	ft := &respondingTransport{}
	a := newTestAdapter(nil, ft)

	if err := a.WatchDevice(context.Background(), "+"); !errors.Is(err, ErrInvalidTopicSegment) {
		t.Errorf("WatchDevice(\"+\") = %v, want ErrInvalidTopicSegment", err)
	}
	if _, err := a.ReadProperty(context.Background(), "+", "temperature"); !errors.Is(err, ErrInvalidTopicSegment) {
		t.Errorf("ReadProperty(deviceID=\"+\") = %v, want ErrInvalidTopicSegment", err)
	}
	if _, err := a.ReadProperty(context.Background(), "dev-1", "#"); !errors.Is(err, ErrInvalidTopicSegment) {
		t.Errorf("ReadProperty(path=\"#\") = %v, want ErrInvalidTopicSegment", err)
	}
	if err := a.WriteProperty(context.Background(), "+", "led", api.BoolValue(true)); !errors.Is(err, ErrInvalidTopicSegment) {
		t.Errorf("WriteProperty(deviceID=\"+\") = %v, want ErrInvalidTopicSegment", err)
	}
}

func TestAdapter_CircuitBreakerOpensAfterConsecutiveReadFailures(t *testing.T) {
	ft := &respondingTransport{publishErr: errors.New("broker unreachable")}
	a := newTestAdapter(nil, ft, WithRequestTimeout(50*time.Millisecond))

	for i := 0; i < circuitBreakerMaxErrors; i++ {
		if _, err := a.ReadProperty(context.Background(), "dev-1", "temperature"); err == nil {
			t.Fatalf("attempt %d: expected an error", i)
		}
	}

	_, err := a.ReadProperty(context.Background(), "dev-1", "temperature")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("ReadProperty after %d consecutive failures = %v, want ErrCircuitOpen", circuitBreakerMaxErrors, err)
	}
}

func TestAdapter_CircuitBreakerClosesAfterSuccess(t *testing.T) {
	ft := &respondingTransport{}
	a := newTestAdapter(nil, ft, WithRequestTimeout(50*time.Millisecond))

	ft.mu.Lock()
	ft.publishErr = errors.New("broker unreachable")
	ft.mu.Unlock()
	for i := 0; i < circuitBreakerMaxErrors-1; i++ {
		_, _ = a.ReadProperty(context.Background(), "dev-1", "temperature")
	}

	ft.mu.Lock()
	ft.publishErr = nil
	ft.mu.Unlock()
	ft.onPublish = func(topic string, _ []byte) {
		if strings.HasSuffix(topic, "/get") {
			a.dispatch(topicProps("dev-1", "temperature"), []byte(`{"float":1}`))
		}
	}
	if _, err := a.ReadProperty(context.Background(), "dev-1", "temperature"); err != nil {
		t.Fatalf("ReadProperty after recovery: %v", err)
	}

	// The success above should have reset the consecutive-failure count, so
	// the breaker shouldn't be anywhere near open.
	ft.publishErr = errors.New("broker unreachable")
	if _, err := a.ReadProperty(context.Background(), "dev-1", "temperature"); errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("ReadProperty = %v, breaker should not be open right after a success reset the count", err)
	}
}
