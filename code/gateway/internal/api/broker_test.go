package api_test

import (
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

func TestBrokerPublishSubscribe(t *testing.T) {
	b := api.NewBroker()
	ch, unsubscribe := b.Subscribe("dev-1")
	defer unsubscribe()

	update := api.PropertyUpdate{DeviceID: "dev-1", PropertyPath: "temperature", Timestamp: time.Now()}
	b.Publish(update)

	select {
	case got := <-ch:
		if got.PropertyPath != "temperature" {
			t.Errorf("PropertyPath = %q, want %q", got.PropertyPath, "temperature")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published update")
	}
}

func TestBrokerFanOutToMultipleSubscribers(t *testing.T) {
	b := api.NewBroker()
	ch1, unsub1 := b.Subscribe("dev-1")
	defer unsub1()
	ch2, unsub2 := b.Subscribe("dev-1")
	defer unsub2()

	b.Publish(api.PropertyUpdate{DeviceID: "dev-1", PropertyPath: "p"})

	for i, ch := range []<-chan api.PropertyUpdate{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive the update", i)
		}
	}
}

func TestBrokerIgnoresOtherDevices(t *testing.T) {
	b := api.NewBroker()
	ch, unsubscribe := b.Subscribe("dev-1")
	defer unsubscribe()

	b.Publish(api.PropertyUpdate{DeviceID: "dev-2", PropertyPath: "p"})

	select {
	case got := <-ch:
		t.Fatalf("expected no update, got %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestBrokerUnsubscribeStopsDelivery(t *testing.T) {
	b := api.NewBroker()
	ch, unsubscribe := b.Subscribe("dev-1")
	unsubscribe()

	b.Publish(api.PropertyUpdate{DeviceID: "dev-1", PropertyPath: "p"})

	if _, ok := <-ch; ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}
