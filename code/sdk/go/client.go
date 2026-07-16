package udal

import (
	"context"
	"errors"
	"io"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/structpb"
)

// Client is the application-side SDK (req42.adoc §7.3): reads/writes device
// properties, sends commands, and subscribes to live property updates.
type Client struct {
	conn   *grpc.ClientConn
	stub   udalv1.DeviceServiceClient
	apiKey string
}

// NewClient dials the gateway described by cfg. Call [Client.Close] when done.
func NewClient(cfg ClientConfig) (*Client, error) {
	conn, err := dial(cfg.GatewayURL, cfg.TLSConfig)
	if err != nil {
		return nil, wrapError(err)
	}
	return &Client{conn: conn, stub: udalv1.NewDeviceServiceClient(conn), apiKey: cfg.APIKey}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) authCtx(ctx context.Context) context.Context { return withAPIKey(ctx, c.apiKey) }

// GetProperty reads deviceID's current value at path.
func (c *Client) GetProperty(ctx context.Context, deviceID, path string) (any, error) {
	resp, err := c.stub.GetProperty(c.authCtx(ctx), &udalv1.GetPropertyRequest{DeviceId: deviceID, PropertyPath: path})
	if err != nil {
		return nil, wrapError(err)
	}
	return valueFromProto(resp.GetValue()), nil
}

// WriteProperty writes value to deviceID's property at path.
func (c *Client) WriteProperty(ctx context.Context, deviceID, path string, value any) error {
	pv, err := valueToProto(value)
	if err != nil {
		return &Error{Code: codes.InvalidArgument, Message: err.Error()}
	}
	_, err = c.stub.SetProperty(c.authCtx(ctx), &udalv1.SetPropertyRequest{DeviceId: deviceID, PropertyPath: path, Value: pv})
	return wrapError(err)
}

// SendCommand sends a named command with params to deviceID and returns its result.
func (c *Client) SendCommand(ctx context.Context, deviceID, command string, params map[string]any) (any, error) {
	s, err := structpb.NewStruct(params)
	if err != nil {
		return nil, &Error{Code: codes.InvalidArgument, Message: err.Error()}
	}
	resp, err := c.stub.SendCommand(c.authCtx(ctx), &udalv1.SendCommandRequest{DeviceId: deviceID, Command: command, Params: s})
	if err != nil {
		return nil, wrapError(err)
	}
	return resp.GetResult().AsInterface(), nil
}

// PropertyUpdate is one event delivered by [Client.Subscribe].
type PropertyUpdate struct {
	DeviceID     string
	PropertyPath string
	Value        any
	Timestamp    time.Time
}

// Subscribe streams property updates for deviceID (every property if path
// is ""). The returned channel closes when ctx is done or the stream ends;
// errCh (buffered, capacity 1) receives at most one terminal error — read
// it only after the update channel closes.
func (c *Client) Subscribe(ctx context.Context, deviceID, path string) (<-chan PropertyUpdate, <-chan error) {
	updates := make(chan PropertyUpdate)
	errCh := make(chan error, 1)
	go func() {
		defer close(updates)
		stream, err := c.stub.Subscribe(c.authCtx(ctx), &udalv1.SubscribeRequest{DeviceId: deviceID, PropertyPath: path})
		if err != nil {
			errCh <- wrapError(err)
			return
		}
		for {
			ev, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					errCh <- wrapError(err)
				}
				return
			}
			update := PropertyUpdate{
				DeviceID:     ev.GetDeviceId(),
				PropertyPath: ev.GetPropertyPath(),
				Value:        valueFromProto(ev.GetValue()),
				Timestamp:    ev.GetTimestamp().AsTime(),
			}
			select {
			case updates <- update:
			case <-ctx.Done():
				return
			}
		}
	}()
	return updates, errCh
}
