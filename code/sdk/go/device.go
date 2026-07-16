package udal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// Params holds a command's parameters, as delivered to a [CommandHandler].
type Params map[string]any

// Float returns key's value as a float64, or 0 if absent or not a number.
func (p Params) Float(key string) float64 {
	v, _ := p[key].(float64)
	return v
}

// String returns key's value as a string, or "" if absent or not a string.
func (p Params) String(key string) string {
	v, _ := p[key].(string)
	return v
}

// Bool returns key's value as a bool, or false if absent or not a bool.
func (p Params) Bool(key string) bool {
	v, _ := p[key].(bool)
	return v
}

// CommandHandler processes one command and returns a result (may be nil)
// or an error. A non-nil error is reported to the gateway as a device NACK
// (FAILED_PRECONDITION on the SendCommand caller's side), with the error's
// message included.
type CommandHandler func(params Params) (any, error)

// Device is the device-side SDK (req42.adoc §7.3): registers with a
// gateway, publishes property values, and handles incoming commands.
// Devices using this SDK connect directly over gRPC — there is no
// transport adapter in between — so commands are delivered over
// StreamCommands rather than through MQTT/HTTP/CAN.
type Device struct {
	cfg  Config
	conn *grpc.ClientConn
	stub udalv1.DeviceServiceClient
	log  *slog.Logger

	mu       sync.RWMutex
	deviceID string
	handlers map[string]CommandHandler
}

// NewDevice dials the gateway described by cfg. Call [Device.Run] to
// register and start handling commands; [Device.OnCommand] may be called
// any time before or after Run starts.
func NewDevice(cfg Config) (*Device, error) {
	conn, err := dial(cfg.GatewayURL, cfg.TLSConfig)
	if err != nil {
		return nil, wrapError(err)
	}
	return &Device{
		cfg:      cfg,
		conn:     conn,
		stub:     udalv1.NewDeviceServiceClient(conn),
		log:      slog.Default(),
		deviceID: cfg.DeviceID,
		handlers: make(map[string]CommandHandler),
	}, nil
}

// Close closes the underlying gRPC connection.
func (d *Device) Close() error { return d.conn.Close() }

// ID returns the device's ID: cfg.DeviceID if it was set, otherwise the
// gateway-assigned ID once Run has registered successfully.
func (d *Device) ID() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.deviceID
}

// OnCommand registers handler for the named command, replacing any handler
// previously registered for that name.
func (d *Device) OnCommand(name string, handler CommandHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[name] = handler
}

func (d *Device) authCtx(ctx context.Context) context.Context { return withAPIKey(ctx, d.cfg.APIKey) }

func (d *Device) register(ctx context.Context) error {
	resp, err := d.stub.RegisterDevice(d.authCtx(ctx), &udalv1.RegisterDeviceRequest{
		Id:         d.cfg.DeviceID,
		Name:       d.cfg.Name,
		Capability: d.cfg.Capability,
		Transport:  d.cfg.Transport,
		Labels:     d.cfg.Labels,
	})
	if err != nil {
		// Already registered under our own stable DeviceID (e.g. this is a
		// reconnect after a process restart) isn't a failure to give up on.
		if status.Code(err) == codes.AlreadyExists && d.cfg.DeviceID != "" {
			d.mu.Lock()
			d.deviceID = d.cfg.DeviceID
			d.mu.Unlock()
			return nil
		}
		return wrapError(err)
	}
	d.mu.Lock()
	d.deviceID = resp.GetDevice().GetId()
	d.mu.Unlock()
	return nil
}

// PublishProperty writes a value to one of this device's own properties.
func (d *Device) PublishProperty(ctx context.Context, path string, value any) error {
	pv, err := valueToProto(value)
	if err != nil {
		return &Error{Code: codes.InvalidArgument, Message: err.Error()}
	}
	_, err = d.stub.SetProperty(d.authCtx(ctx), &udalv1.SetPropertyRequest{
		DeviceId: d.ID(), PropertyPath: path, Value: pv,
	})
	return wrapError(err)
}

// Run registers the device (if not already) and opens its command stream,
// re-registering and reconnecting with exponential backoff (1s up to 30s)
// if the connection is lost, until ctx is canceled. It blocks until then.
func (d *Device) Run(ctx context.Context) error {
	if err := d.register(ctx); err != nil {
		return err
	}

	const (
		baseBackoff = time.Second
		maxBackoff  = 30 * time.Second
	)
	backoff := baseBackoff
	for ctx.Err() == nil {
		connectedAt := time.Now()
		err := d.runCommandStream(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(connectedAt) > 5*time.Second {
			// The stream was healthy for a while before failing; treat this
			// as a fresh outage rather than compounding backoff from a
			// previous one.
			backoff = baseBackoff
		}
		d.log.Warn("command stream disconnected, reconnecting", "err", err, "backoff", backoff)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		_ = d.register(ctx) // covers a gateway restart with a non-persistent registry; no-op otherwise
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	return nil
}

func (d *Device) runCommandStream(ctx context.Context) error {
	streamCtx := metadata.AppendToOutgoingContext(d.authCtx(ctx), "x-device-id", d.ID())
	stream, err := d.stub.StreamCommands(streamCtx)
	if err != nil {
		return err
	}
	for {
		cmd, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		go d.handleCommand(stream, cmd)
	}
}

func (d *Device) handleCommand(stream udalv1.DeviceService_StreamCommandsClient, cmd *udalv1.Command) {
	d.mu.RLock()
	handler, ok := d.handlers[cmd.GetName()]
	d.mu.RUnlock()

	result := &udalv1.CommandResult{Id: cmd.GetId()}
	switch {
	case !ok:
		result.Error = fmt.Sprintf("no handler registered for command %q", cmd.GetName())
	default:
		out, err := handler(Params(cmd.GetParams().AsMap()))
		if err != nil {
			result.Error = err.Error()
			break
		}
		result.Success = true
		if out != nil {
			v, verr := structpb.NewValue(out)
			if verr != nil {
				result.Success = false
				result.Error = fmt.Sprintf("encode result: %v", verr)
			} else {
				result.Result = v
			}
		}
	}
	_ = stream.Send(result) // best-effort; a broken stream surfaces via the next Recv in runCommandStream
}
