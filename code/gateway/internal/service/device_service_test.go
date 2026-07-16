package service_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func newSvc() *service.DeviceService {
	return service.New(registry.NewMemoryRegistry(), api.NewMemoryPropertyStore(), api.NewBroker(), api.NewCommandRouter())
}

// adminCtx returns a context carrying an authenticated admin identity —
// these tests exercise RPC-handler behavior, not RBAC/ACL (see
// internal/auth's own tests for that), so admin (which the RBAC matrix
// allows everywhere) keeps them focused on their original concern.
func adminCtx() context.Context {
	return auth.ContextWithIdentity(context.Background(), auth.Identity{Subject: "test-admin", Role: auth.RoleAdmin})
}

func grpcCode(err error) codes.Code {
	s, _ := status.FromError(err)
	return s.Code()
}

// ─── RegisterDevice ───────────────────────────────────────────────────────────

func TestRegisterDevice_OK(t *testing.T) {
	svc := newSvc()
	resp, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name:       "sensor-1",
		Capability: "temperature-sensor",
		Transport:  "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if resp.Device.Id == "" {
		t.Error("expected non-empty device ID")
	}
	if resp.Device.Status != udalv1.DeviceStatus_DEVICE_STATUS_UNKNOWN {
		t.Errorf("initial status = %v, want UNKNOWN", resp.Device.Status)
	}
}

func TestRegisterDevice_MissingFields(t *testing.T) {
	svc := newSvc()
	tests := []struct {
		name string
		req  *udalv1.RegisterDeviceRequest
	}{
		{"no name", &udalv1.RegisterDeviceRequest{Capability: "c", Transport: "mqtt"}},
		{"no capability", &udalv1.RegisterDeviceRequest{Name: "n", Transport: "mqtt"}},
		{"no transport", &udalv1.RegisterDeviceRequest{Name: "n", Capability: "c"}},
	}
	for _, tt := range tests {
		_, err := svc.RegisterDevice(adminCtx(), tt.req)
		if grpcCode(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", tt.name, err)
		}
	}
}

func TestRegisterDevice_CustomID(t *testing.T) {
	svc := newSvc()
	resp, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Id: "sensor-01", Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if resp.GetDevice().GetId() != "sensor-01" {
		t.Errorf("Id = %q, want %q", resp.GetDevice().GetId(), "sensor-01")
	}

	_, err = svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Id: "sensor-01", Name: "sensor-again", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if grpcCode(err) != codes.AlreadyExists {
		t.Errorf("duplicate custom id: expected AlreadyExists, got %v", err)
	}
}

// ─── GetDevice ────────────────────────────────────────────────────────────────

func TestGetDevice_OK(t *testing.T) {
	svc := newSvc()
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "cam", Capability: "ip-camera", Transport: "http",
	})
	got, err := svc.GetDevice(adminCtx(), &udalv1.GetDeviceRequest{Id: reg.Device.Id})
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if got.Device.Name != "cam" {
		t.Errorf("Name = %q, want %q", got.Device.Name, "cam")
	}
}

func TestGetDevice_NotFound(t *testing.T) {
	svc := newSvc()
	_, err := svc.GetDevice(adminCtx(), &udalv1.GetDeviceRequest{Id: "missing"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestGetDevice_EmptyID(t *testing.T) {
	svc := newSvc()
	_, err := svc.GetDevice(adminCtx(), &udalv1.GetDeviceRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
}

// ─── ListDevices ──────────────────────────────────────────────────────────────

func TestListDevices(t *testing.T) {
	svc := newSvc()
	for _, d := range []struct{ name, cap, tr string }{
		{"s1", "temperature-sensor", "mqtt"},
		{"s2", "temperature-sensor", "mqtt"},
		{"c1", "ip-camera", "http"},
	} {
		if _, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
			Name: d.name, Capability: d.cap, Transport: d.tr,
		}); err != nil {
			t.Fatalf("RegisterDevice %s: %v", d.name, err)
		}
	}

	all, _ := svc.ListDevices(adminCtx(), &udalv1.ListDevicesRequest{})
	if len(all.Devices) != 3 {
		t.Errorf("all: got %d, want 3", len(all.Devices))
	}

	byCap, _ := svc.ListDevices(adminCtx(), &udalv1.ListDevicesRequest{Capability: "temperature-sensor"})
	if len(byCap.Devices) != 2 {
		t.Errorf("by capability: got %d, want 2", len(byCap.Devices))
	}
}

// ─── DeleteDevice ─────────────────────────────────────────────────────────────

func TestDeleteDevice(t *testing.T) {
	svc := newSvc()
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "x", Capability: "c", Transport: "mqtt",
	})
	if _, err := svc.DeleteDevice(adminCtx(), &udalv1.DeleteDeviceRequest{Id: reg.Device.Id}); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}
	_, err := svc.GetDevice(adminCtx(), &udalv1.GetDeviceRequest{Id: reg.Device.Id})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("after delete: expected NotFound, got %v", err)
	}

	// delete non-existent
	_, err = svc.DeleteDevice(adminCtx(), &udalv1.DeleteDeviceRequest{Id: "nope"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("delete missing: expected NotFound, got %v", err)
	}
}

// ─── GetProperty / SetProperty ────────────────────────────────────────────────

func TestSetGetProperty(t *testing.T) {
	svc := newSvc()
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "sensor", Capability: "temperature-sensor", Transport: "mqtt",
	})
	devID := reg.Device.Id

	setResp, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId:     devID,
		PropertyPath: "temperature",
		Value:        &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 23.5}},
	})
	if err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	if setResp.NewValue.GetFloatVal() != 23.5 {
		t.Errorf("SetProperty response: FloatVal = %v, want 23.5", setResp.NewValue.GetFloatVal())
	}

	getResp, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: devID, PropertyPath: "temperature",
	})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if getResp.Value.GetFloatVal() != 23.5 {
		t.Errorf("GetProperty: FloatVal = %v, want 23.5", getResp.Value.GetFloatVal())
	}
}

func TestGetProperty_DeviceNotFound(t *testing.T) {
	svc := newSvc()
	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: "missing", PropertyPath: "temperature",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestGetProperty_PropertyNotFound(t *testing.T) {
	svc := newSvc()
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	_, err := svc.GetProperty(adminCtx(), &udalv1.GetPropertyRequest{
		DeviceId: reg.Device.Id, PropertyPath: "nonexistent",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestSetProperty_EmptyArgs(t *testing.T) {
	svc := newSvc()
	tests := []struct {
		name string
		req  *udalv1.SetPropertyRequest
	}{
		{"no device_id", &udalv1.SetPropertyRequest{PropertyPath: "p"}},
		{"no property_path", &udalv1.SetPropertyRequest{DeviceId: "d"}},
	}
	for _, tt := range tests {
		_, err := svc.SetProperty(adminCtx(), tt.req)
		if grpcCode(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", tt.name, err)
		}
	}
}

// ─── SendCommand ──────────────────────────────────────────────────────────────

func TestSendCommand_DeviceNotFound(t *testing.T) {
	svc := newSvc()
	_, err := svc.SendCommand(adminCtx(), &udalv1.SendCommandRequest{
		DeviceId: "missing", Command: "reboot",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestSendCommand_Unimplemented(t *testing.T) {
	svc := newSvc()
	reg, _ := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	_, err := svc.SendCommand(adminCtx(), &udalv1.SendCommandRequest{
		DeviceId: reg.Device.Id, Command: "reboot",
	})
	if grpcCode(err) != codes.Unimplemented {
		t.Errorf("expected Unimplemented, got %v", err)
	}
}

// fakeStreamCommandsStream implements udalv1.DeviceService_StreamCommandsServer
// without a real network connection: Send delivers a Command to sendCh (for
// the test's simulated "device" goroutine to read), Recv reads a
// CommandResult the test pushes onto recvCh.
type fakeStreamCommandsStream struct {
	grpc.ServerStream
	ctx    context.Context
	sendCh chan *udalv1.Command
	recvCh chan *udalv1.CommandResult
}

func (f *fakeStreamCommandsStream) Context() context.Context { return f.ctx }

func (f *fakeStreamCommandsStream) Send(c *udalv1.Command) error {
	select {
	case f.sendCh <- c:
		return nil
	case <-f.ctx.Done():
		return f.ctx.Err()
	}
}

func (f *fakeStreamCommandsStream) Recv() (*udalv1.CommandResult, error) {
	select {
	case r, ok := <-f.recvCh:
		if !ok {
			return nil, io.EOF
		}
		return r, nil
	case <-f.ctx.Done():
		return nil, f.ctx.Err()
	}
}

func TestSendCommand_RoutesToConnectedDevice(t *testing.T) {
	svc := newSvc()
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := reg.GetDevice().GetId()

	ctx, cancel := context.WithCancel(adminCtx())
	defer cancel()
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-device-id", deviceID))
	stream := &fakeStreamCommandsStream{
		ctx:    ctx,
		sendCh: make(chan *udalv1.Command, 1),
		recvCh: make(chan *udalv1.CommandResult, 1),
	}

	streamDone := make(chan error, 1)
	go func() { streamDone <- svc.StreamCommands(stream) }()
	time.Sleep(50 * time.Millisecond) // let StreamCommands register before dispatching

	// Simulated device: echo back a success result for whatever it receives.
	go func() {
		cmd := <-stream.sendCh
		result, _ := structpb.NewValue(map[string]any{"offset": 1.5})
		stream.recvCh <- &udalv1.CommandResult{Id: cmd.GetId(), Success: true, Result: result}
	}()

	resp, err := svc.SendCommand(adminCtx(), &udalv1.SendCommandRequest{
		DeviceId: deviceID, Command: "calibrate",
	})
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if got := resp.GetResult().AsInterface(); fmt.Sprint(got) != fmt.Sprint(map[string]any{"offset": 1.5}) {
		t.Errorf("SendCommand result = %v, want {offset:1.5}", got)
	}

	cancel()
	<-streamDone
}

func TestSendCommand_DeviceNACK(t *testing.T) {
	svc := newSvc()
	reg, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	deviceID := reg.GetDevice().GetId()

	ctx, cancel := context.WithCancel(adminCtx())
	defer cancel()
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-device-id", deviceID))
	stream := &fakeStreamCommandsStream{
		ctx:    ctx,
		sendCh: make(chan *udalv1.Command, 1),
		recvCh: make(chan *udalv1.CommandResult, 1),
	}
	go func() { _ = svc.StreamCommands(stream) }()
	time.Sleep(50 * time.Millisecond) // let StreamCommands register before dispatching
	go func() {
		cmd := <-stream.sendCh
		stream.recvCh <- &udalv1.CommandResult{Id: cmd.GetId(), Success: false, Error: "out of range"}
	}()

	_, err = svc.SendCommand(adminCtx(), &udalv1.SendCommandRequest{DeviceId: deviceID, Command: "calibrate"})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", err)
	}
}

func TestSendCommand_EmptyArgs(t *testing.T) {
	svc := newSvc()
	_, err := svc.SendCommand(adminCtx(), &udalv1.SendCommandRequest{Command: "c"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("device_id missing: expected InvalidArgument, got %v", err)
	}
	_, err = svc.SendCommand(adminCtx(), &udalv1.SendCommandRequest{DeviceId: "d"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("command missing: expected InvalidArgument, got %v", err)
	}
}

// ─── Subscribe ────────────────────────────────────────────────────────────────

// fakeSubscribeStream implements udalv1.DeviceService_SubscribeServer without a
// real network connection, embedding a nil grpc.ServerStream since the handler
// only calls Context() and Send(). Send runs on the Subscribe goroutine while
// the test reads sent() from the main goroutine, so access is mutex-guarded.
type fakeSubscribeStream struct {
	grpc.ServerStream
	ctx context.Context

	mu   sync.Mutex
	msgs []*udalv1.SubscribeResponse
}

func (f *fakeSubscribeStream) Context() context.Context { return f.ctx }

func (f *fakeSubscribeStream) Send(r *udalv1.SubscribeResponse) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, r)
	return nil
}

func (f *fakeSubscribeStream) sent() []*udalv1.SubscribeResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*udalv1.SubscribeResponse(nil), f.msgs...)
}

func TestSubscribe_DeviceNotFound(t *testing.T) {
	svc := newSvc()
	ctx, cancel := context.WithCancel(adminCtx())
	defer cancel()
	err := svc.Subscribe(&udalv1.SubscribeRequest{DeviceId: "missing"}, &fakeSubscribeStream{ctx: ctx})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestSubscribe_EmptyDeviceID(t *testing.T) {
	svc := newSvc()
	ctx, cancel := context.WithCancel(adminCtx())
	defer cancel()
	err := svc.Subscribe(&udalv1.SubscribeRequest{}, &fakeSubscribeStream{ctx: ctx})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
}

func TestSubscribe_ReceivesPublishedUpdate(t *testing.T) {
	svc := newSvc()
	dev, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	ctx, cancel := context.WithCancel(adminCtx())
	stream := &fakeSubscribeStream{ctx: ctx}

	done := make(chan error, 1)
	go func() {
		done <- svc.Subscribe(&udalv1.SubscribeRequest{DeviceId: dev.GetDevice().GetId()}, stream)
	}()

	// Give Subscribe time to register with the broker before publishing.
	time.Sleep(50 * time.Millisecond)

	_, err = svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId:     dev.GetDevice().GetId(),
		PropertyPath: "temperature",
		Value:        &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 21.5}},
	})
	if err != nil {
		t.Fatalf("SetProperty: %v", err)
	}

	deadline := time.After(time.Second)
	for len(stream.sent()) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Subscribe to receive the update")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Subscribe returned error after cancel: %v", err)
	}

	if got := stream.sent()[0]; got.GetPropertyPath() != "temperature" || got.GetValue().GetFloatVal() != 21.5 {
		t.Errorf("unexpected event: %+v", got)
	}
}

func TestSubscribe_FiltersByPropertyPath(t *testing.T) {
	svc := newSvc()
	dev, err := svc.RegisterDevice(adminCtx(), &udalv1.RegisterDeviceRequest{
		Name: "s", Capability: "temperature-sensor", Transport: "mqtt",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	ctx, cancel := context.WithCancel(adminCtx())
	stream := &fakeSubscribeStream{ctx: ctx}

	done := make(chan error, 1)
	go func() {
		done <- svc.Subscribe(&udalv1.SubscribeRequest{DeviceId: dev.GetDevice().GetId(), PropertyPath: "humidity"}, stream)
	}()
	time.Sleep(50 * time.Millisecond)

	if _, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId:     dev.GetDevice().GetId(),
		PropertyPath: "temperature",
		Value:        &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 1}},
	}); err != nil {
		t.Fatalf("SetProperty temperature: %v", err)
	}
	if _, err := svc.SetProperty(adminCtx(), &udalv1.SetPropertyRequest{
		DeviceId:     dev.GetDevice().GetId(),
		PropertyPath: "humidity",
		Value:        &udalv1.PropertyValue{Value: &udalv1.PropertyValue_FloatVal{FloatVal: 2}},
	}); err != nil {
		t.Fatalf("SetProperty humidity: %v", err)
	}

	deadline := time.After(time.Second)
	for len(stream.sent()) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the filtered update")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	<-done

	if got := stream.sent(); len(got) != 1 || got[0].GetPropertyPath() != "humidity" {
		t.Errorf("expected exactly one humidity event, got %+v", got)
	}
}
