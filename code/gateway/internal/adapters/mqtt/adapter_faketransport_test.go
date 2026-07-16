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
	mu         sync.Mutex
	publishErr error
	onPublish  func(topic string, payload []byte)
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
func (f *respondingTransport) Subscribe(context.Context, string) error { return nil }
func (f *respondingTransport) Disconnect(context.Context) error        { return nil }

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
