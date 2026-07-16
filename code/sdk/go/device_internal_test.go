package udal

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeDeviceServiceClient implements udalv1.DeviceServiceClient with just
// enough behavior to test Device.Run's reconnect loop without a real
// network connection: RegisterDevice always succeeds, StreamCommands fails
// failFirstN times before succeeding with a stream that blocks until its
// context is done (embeds a nil DeviceServiceClient for every other method,
// none of which Run/runCommandStream/register ever call).
type fakeDeviceServiceClient struct {
	udalv1.DeviceServiceClient
	streamAttempts int32
	failFirstN     int32
}

func (f *fakeDeviceServiceClient) RegisterDevice(_ context.Context, _ *udalv1.RegisterDeviceRequest, _ ...grpc.CallOption) (*udalv1.RegisterDeviceResponse, error) {
	return &udalv1.RegisterDeviceResponse{Device: &udalv1.Device{Id: "dev-test"}}, nil
}

func (f *fakeDeviceServiceClient) StreamCommands(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[udalv1.CommandResult, udalv1.Command], error) {
	if atomic.AddInt32(&f.streamAttempts, 1) <= f.failFirstN {
		return nil, status.Error(codes.Unavailable, "simulated outage")
	}
	return &fakeCommandStream{ctx: ctx}, nil
}

// fakeCommandStream blocks on Recv until its context is canceled, simulating
// a healthy, idle command stream.
type fakeCommandStream struct {
	grpc.ClientStream
	ctx context.Context
}

func (s *fakeCommandStream) Send(*udalv1.CommandResult) error { return nil }

func (s *fakeCommandStream) Recv() (*udalv1.Command, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

// TestDevice_RunReconnectsAfterStreamFailures exercises the #12 AC
// "Reconnect: gateway outage → SDK reconnects, resumes without manual
// intervention" — using a fake stub rather than a real ~30s network outage,
// since what actually needs verifying is that Run's retry loop keeps trying
// with backoff and recovers on its own, not that it survives a literal
// half-minute of wall-clock time.
func TestDevice_RunReconnectsAfterStreamFailures(t *testing.T) {
	fake := &fakeDeviceServiceClient{failFirstN: 2}
	d := &Device{
		cfg:      Config{DeviceID: "dev-test"},
		stub:     fake,
		log:      slog.Default(),
		handlers: make(map[string]CommandHandler),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Backoff sequence for 2 failures before success: fail, wait 1s, fail,
	// wait 2s, succeed — allow generous margin above the 3s minimum.
	time.Sleep(3500 * time.Millisecond)
	if got := atomic.LoadInt32(&fake.streamAttempts); got < 3 {
		t.Fatalf("expected at least 3 StreamCommands attempts (2 failures + 1 success), got %d", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned an error after cancel: %v", err)
	}
}
