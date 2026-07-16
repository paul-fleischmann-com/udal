package api_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/paulefl/udal/code/gateway/internal/api"
)

func TestCommandRouter_DispatchAndResult(t *testing.T) {
	r := api.NewCommandRouter()
	commands, unregister := r.Register("dev-1")
	defer unregister()

	go func() {
		cmd := <-commands
		r.SubmitResult(api.CommandResult{ID: cmd.ID, Success: true, Result: "ok"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := r.Dispatch(ctx, "dev-1", api.Command{ID: api.NewCommandID(), Name: "calibrate"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !res.Success || res.Result != "ok" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestCommandRouter_NotConnected(t *testing.T) {
	r := api.NewCommandRouter()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := r.Dispatch(ctx, "missing", api.Command{ID: api.NewCommandID(), Name: "x"})
	if !errors.Is(err, api.ErrDeviceNotConnected) {
		t.Errorf("expected ErrDeviceNotConnected, got %v", err)
	}
}

func TestCommandRouter_Timeout(t *testing.T) {
	r := api.NewCommandRouter()
	_, unregister := r.Register("dev-1")
	defer unregister()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// Nobody reads the channel or submits a result, so this should time out
	// via ctx rather than hang.
	_, err := r.Dispatch(ctx, "dev-1", api.Command{ID: api.NewCommandID(), Name: "x"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestCommandRouter_ConcurrentCommandsCorrelateIndependently(t *testing.T) {
	r := api.NewCommandRouter()
	commands, unregister := r.Register("dev-1")
	defer unregister()

	go func() {
		for i := 0; i < 2; i++ {
			cmd := <-commands
			// Reply with the command's own name so the test can verify
			// each Dispatch got its own result, not a mixed-up one.
			r.SubmitResult(api.CommandResult{ID: cmd.ID, Success: true, Result: cmd.Name})
		}
	}()

	type outcome struct {
		name string
		got  string
		err  error
	}
	results := make(chan outcome, 2)
	for _, name := range []string{"a", "b"} {
		go func(name string) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			res, err := r.Dispatch(ctx, "dev-1", api.Command{ID: api.NewCommandID(), Name: name})
			got, _ := res.Result.(string)
			results <- outcome{name: name, got: got, err: err}
		}(name)
	}

	for i := 0; i < 2; i++ {
		o := <-results
		if o.err != nil {
			t.Errorf("Dispatch(%s): %v", o.name, o.err)
			continue
		}
		if o.got != o.name {
			t.Errorf("Dispatch(%s): result = %q, want %q (correlation mixed up)", o.name, o.got, o.name)
		}
	}
}

func TestCommandRouter_SubmitResultWithoutWaiterIsNoop(t *testing.T) {
	r := api.NewCommandRouter()
	// No Dispatch is waiting for "unknown" — this must not panic or block.
	r.SubmitResult(api.CommandResult{ID: "unknown", Success: true})
}

func TestCommandRouter_UnregisterStopsDelivery(t *testing.T) {
	r := api.NewCommandRouter()
	commands, unregister := r.Register("dev-1")
	unregister()

	if _, ok := <-commands; ok {
		t.Error("expected channel to be closed after unregister")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := r.Dispatch(ctx, "dev-1", api.Command{ID: api.NewCommandID(), Name: "x"})
	if !errors.Is(err, api.ErrDeviceNotConnected) {
		t.Errorf("expected ErrDeviceNotConnected after unregister, got %v", err)
	}
}
