package service_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	httpadapter "github.com/paulefl/udal/code/gateway/internal/adapters/http"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
)

// TestHTTPAdapter_EndToEnd chains a real httpadapter.Adapter (not a fake)
// through DeviceService the same way cmd/gateway/main.go wires it, against
// a simulated device (httptest.Server) — the doc-check plan
// (docs/features/plans/24-http-adapter-poll-webhook-mtls.md,
// "E2E-Testabdeckung") flagged this: the fakeHTTPAdapter-based tests in
// device_service_http_test.go only prove DeviceService's routing logic,
// not that a real poll/webhook round trip actually reaches a Subscribe
// stream the way a real client's shell of RegisterDevice -> GetProperty /
// Subscribe would.
func TestHTTPAdapter_EndToEnd(t *testing.T) {
	deviceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/properties/humidity":
			_, _ = w.Write([]byte(`{"int":55}`))
		case "/properties":
			// Bulk snapshot the background poller hits — empty is fine,
			// this test's poll-path assertion goes through the on-demand
			// GetProperty path (/properties/humidity) below, not the
			// poller; the webhook path is exercised separately.
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer deviceSrv.Close()

	reg := registry.NewMemoryRegistry()
	broker := api.NewBroker()
	svc := service.New(reg, api.NewMemoryPropertyStore(), broker, api.NewCommandRouter())

	httpAdapter := httpadapter.New(func(deviceID, path string, v api.PropertyValue) {
		broker.Publish(api.PropertyUpdate{DeviceID: deviceID, PropertyPath: path, Value: v, Timestamp: time.Now()})
	})
	svc.SetHTTPAdapter(httpAdapter)
	defer httpAdapter.Close()

	regResp, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "http",
		Labels: map[string]string{httpadapter.LabelEndpoint: deviceSrv.URL},
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := regResp.Device.Id

	// ── Poll chain: GetProperty issues a real GET against deviceSrv ──────
	got, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{DeviceId: deviceID, PropertyPath: "humidity"})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if got.Value.GetIntVal() != 55 {
		t.Errorf("GetProperty humidity = %d, want 55", got.Value.GetIntVal())
	}

	// ── Webhook chain: a real HTTP POST through the adapter's own Handler
	//    (as main.go's webhook server would route it) -> onUpdate ->
	//    Router (broker.Publish) -> an open Subscribe stream ────────────
	ctx, cancel := context.WithCancel(adminCtx())
	defer cancel()
	stream := &fakeSubscribeStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- svc.Subscribe(&udalv1.SubscribeRequest{DeviceId: deviceID}, stream)
	}()

	// Give Subscribe time to register with the broker before the push
	// arrives — same race every other Subscribe test in this package
	// accepts (see TestSubscribe_ReceivesPublishedUpdate).
	time.Sleep(50 * time.Millisecond)

	webhookReq := httptest.NewRequest(http.MethodPost, "/devices/"+deviceID+"/events",
		strings.NewReader(`{"path":"door_open","value":{"bool":true}}`))
	w := httptest.NewRecorder()
	httpAdapter.Handler().ServeHTTP(w, webhookReq)
	if w.Code != http.StatusNoContent {
		t.Fatalf("webhook POST status = %d, want 204; body: %s", w.Code, w.Body.String())
	}

	deadline := time.After(time.Second)
	for len(stream.sent()) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the webhook push to reach the Subscribe stream")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Subscribe returned error after cancel: %v", err)
	}

	if got := stream.sent()[0]; got.GetPropertyPath() != "door_open" || !got.GetValue().GetBoolVal() {
		t.Errorf("unexpected event from webhook push: %+v", got)
	}
}
