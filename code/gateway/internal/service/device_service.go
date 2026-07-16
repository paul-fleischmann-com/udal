// Package service implements the gRPC DeviceService using the internal
// registry and property store.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	mqttadapter "github.com/paulefl/udal/code/gateway/internal/adapters/mqtt"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MQTTAdapter is the subset of *mqttadapter.Adapter's API DeviceService
// needs to route GetProperty/SetProperty for mqtt-transport devices (issue
// #11). A narrow interface rather than the concrete type so tests can
// supply a fake.
type MQTTAdapter interface {
	ReadProperty(ctx context.Context, deviceID, path string) (api.PropertyValue, error)
	WriteProperty(ctx context.Context, deviceID, path string, v api.PropertyValue) error
	WatchDevice(ctx context.Context, deviceID string) error
}

// DeviceService implements udalv1.DeviceServiceServer.
// It delegates device registration to a Registry; property storage goes to
// a PropertyStore for most devices, or to an MQTTAdapter for devices whose
// Transport is "mqtt" (if one is configured — see SetMQTTAdapter). SendCommand
// is routed to a directly-connected gRPC device's StreamCommands channel via
// commands, if one is open for that device; SendCommand-over-MQTT isn't in
// this ticket's scope (no acceptance criterion requires it), so it still
// returns Unimplemented for mqtt-transport devices.
type DeviceService struct {
	udalv1.UnimplementedDeviceServiceServer
	reg      registry.Registry
	props    api.PropertyStore
	broker   *api.Broker
	commands *api.CommandRouter
	mqtt     MQTTAdapter
}

// New returns a DeviceService backed by the given Registry, PropertyStore,
// Broker (fans out Subscribe events), and CommandRouter (routes SendCommand
// to a connected device's StreamCommands channel).
func New(reg registry.Registry, props api.PropertyStore, broker *api.Broker, commands *api.CommandRouter) *DeviceService {
	return &DeviceService{reg: reg, props: props, broker: broker, commands: commands}
}

// SetMQTTAdapter wires an MQTTAdapter into the service so mqtt-transport
// devices' GetProperty/SetProperty route through it instead of the
// in-memory PropertyStore. Optional — a DeviceService without one behaves
// exactly as before (mqtt-transport devices fall through to PropertyStore,
// same as any other transport).
func (s *DeviceService) SetMQTTAdapter(a MQTTAdapter) { s.mqtt = a }

// mqttStatusError maps an MQTTAdapter error to a gRPC status.
func mqttStatusError(err error) error {
	switch {
	case errors.Is(err, mqttadapter.ErrInvalidTopicSegment):
		return status.Errorf(codes.InvalidArgument, "mqtt: %v", err)
	case errors.Is(err, mqttadapter.ErrCircuitOpen):
		return status.Errorf(codes.Unavailable, "mqtt: %v", err)
	case errors.Is(err, context.DeadlineExceeded):
		return status.Errorf(codes.DeadlineExceeded, "mqtt: %v", err)
	default:
		return status.Errorf(codes.Internal, "mqtt: %v", err)
	}
}

// commandTimeout is F-07's "Command timeout (configurable, default 10 s)".
// Not yet wired to gateway config (#41 YAML config isn't done); hardcoded
// default for now.
const commandTimeout = 10 * time.Second

// ─── Authorization helpers ────────────────────────────────────────────────────

// authorize checks the caller (resolved by the AuthN interceptor and stored
// in ctx) against RBAC + d's per-device ACL for op.
func (s *DeviceService) authorize(ctx context.Context, op auth.Operation, d api.Device) error {
	id, ok := auth.Authenticated(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no identity in context")
	}
	return auth.Authorize(id, op, d.ID, d.ACL)
}

// authorizeNoDevice is authorize for operations with no single target
// device (RegisterDevice, ListDevices) — RBAC only, no ACL applies.
func (s *DeviceService) authorizeNoDevice(ctx context.Context, op auth.Operation) error {
	id, ok := auth.Authenticated(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no identity in context")
	}
	return auth.Authorize(id, op, "", nil)
}

// ─── Device registry RPCs ─────────────────────────────────────────────────────

func (s *DeviceService) GetDevice(ctx context.Context, req *udalv1.GetDeviceRequest) (*udalv1.GetDeviceResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	d, err := s.reg.Get(req.GetId())
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "registry get: %v", err)
	}
	if err := s.authorize(ctx, auth.OpGetDevice, d); err != nil {
		return nil, err
	}
	return &udalv1.GetDeviceResponse{Device: toProtoDevice(d)}, nil
}

func (s *DeviceService) ListDevices(ctx context.Context, req *udalv1.ListDevicesRequest) (*udalv1.ListDevicesResponse, error) {
	if err := s.authorizeNoDevice(ctx, auth.OpListDevices); err != nil {
		return nil, err
	}
	devices, err := s.reg.List(registry.ListFilter{
		Capability: req.GetCapability(),
		Transport:  req.GetTransport(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "registry list: %v", err)
	}
	pb := make([]*udalv1.Device, 0, len(devices))
	for _, d := range devices {
		pb = append(pb, toProtoDevice(d))
	}
	return &udalv1.ListDevicesResponse{Devices: pb}, nil
}

func (s *DeviceService) RegisterDevice(ctx context.Context, req *udalv1.RegisterDeviceRequest) (*udalv1.RegisterDeviceResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.GetCapability() == "" {
		return nil, status.Error(codes.InvalidArgument, "capability is required")
	}
	if req.GetTransport() == "" {
		return nil, status.Error(codes.InvalidArgument, "transport is required")
	}
	if err := s.authorizeNoDevice(ctx, auth.OpRegisterDevice); err != nil {
		return nil, err
	}
	d, err := s.reg.Register(api.Device{
		ID:         req.GetId(),
		Name:       req.GetName(),
		Capability: req.GetCapability(),
		Transport:  req.GetTransport(),
		Labels:     req.GetLabels(),
	})
	if err != nil {
		if errors.Is(err, registry.ErrAlreadyExists) {
			return nil, status.Errorf(codes.AlreadyExists, "device already registered: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "registry register: %v", err)
	}
	if d.Transport == "mqtt" && s.mqtt != nil {
		// Best-effort: for most failures, ReadProperty/WriteProperty
		// subscribe lazily on first access regardless, so a failure here
		// just delays fan-out for properties nobody's requested yet. The
		// one failure that doesn't self-heal is an ID containing an MQTT
		// wildcard character ('+'/'#') — mqttadapter.ErrInvalidTopicSegment
		// — which also makes every later ReadProperty/WriteProperty for
		// this device fail the same way, by design (building a topic from
		// such an ID would turn a per-device subscription into a
		// broker-wide wildcard).
		_ = s.mqtt.WatchDevice(ctx, d.ID)
	}
	return &udalv1.RegisterDeviceResponse{Device: toProtoDevice(d)}, nil
}

func (s *DeviceService) DeleteDevice(ctx context.Context, req *udalv1.DeleteDeviceRequest) (*udalv1.DeleteDeviceResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	// DeleteDevice isn't in req42.adoc F-19's RBAC table; treated as
	// admin/operator-only (see plan doc) with no per-device ACL override.
	id, ok := auth.Authenticated(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no identity in context")
	}
	if err := auth.Authorize(id, auth.OpDeleteDevice, req.GetId(), nil); err != nil {
		return nil, err
	}
	if err := s.reg.Delete(req.GetId()); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "registry delete: %v", err)
	}
	return &udalv1.DeleteDeviceResponse{}, nil
}

// ─── Property RPCs ────────────────────────────────────────────────────────────

func (s *DeviceService) GetProperty(ctx context.Context, req *udalv1.GetPropertyRequest) (*udalv1.GetPropertyResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if req.GetPropertyPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "property_path is required")
	}
	d, err := s.reg.Get(req.GetDeviceId())
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetDeviceId())
		}
		return nil, status.Errorf(codes.Internal, "registry get: %v", err)
	}
	if err := s.authorize(ctx, auth.OpGetProperty, d); err != nil {
		return nil, err
	}

	var v api.PropertyValue
	if d.Transport == "mqtt" && s.mqtt != nil {
		v, err = s.mqtt.ReadProperty(ctx, req.GetDeviceId(), req.GetPropertyPath())
		if err != nil {
			return nil, mqttStatusError(err)
		}
	} else {
		v, err = s.props.Get(req.GetDeviceId(), req.GetPropertyPath())
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "property %q not found on device %q", req.GetPropertyPath(), req.GetDeviceId())
		}
	}
	pbVal, err := toProtoValue(v)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode property value: %v", err)
	}
	return &udalv1.GetPropertyResponse{Value: pbVal}, nil
}

func (s *DeviceService) SetProperty(ctx context.Context, req *udalv1.SetPropertyRequest) (*udalv1.SetPropertyResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if req.GetPropertyPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "property_path is required")
	}
	d, err := s.reg.Get(req.GetDeviceId())
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetDeviceId())
		}
		return nil, status.Errorf(codes.Internal, "registry get: %v", err)
	}
	if err := s.authorize(ctx, auth.OpSetProperty, d); err != nil {
		return nil, err
	}
	v := fromProtoValue(req.GetValue())

	if d.Transport == "mqtt" && s.mqtt != nil {
		if err := s.mqtt.WriteProperty(ctx, req.GetDeviceId(), req.GetPropertyPath(), v); err != nil {
			return nil, mqttStatusError(err)
		}
		// No broker.Publish here: the device's own props/{path} publish
		// (echoed after applying the write, or its next heartbeat) drives
		// Subscribe fan-out for mqtt-transport devices, via the adapter's
		// OnPropertyUpdate callback wired up in main.go — the /set/ack
		// this call waited for only confirms receipt, not the device's
		// authoritative new value.
		_ = s.reg.UpdateStatus(req.GetDeviceId(), api.DeviceStatusOnline, time.Now())
		pbVal, _ := toProtoValue(v)
		return &udalv1.SetPropertyResponse{NewValue: pbVal}, nil
	}

	if err := s.props.Set(req.GetDeviceId(), req.GetPropertyPath(), v); err != nil {
		return nil, status.Errorf(codes.Internal, "set property: %v", err)
	}
	now := time.Now()
	_ = s.reg.UpdateStatus(req.GetDeviceId(), api.DeviceStatusOnline, now)
	s.broker.Publish(api.PropertyUpdate{
		DeviceID:     req.GetDeviceId(),
		PropertyPath: req.GetPropertyPath(),
		Value:        v,
		Timestamp:    now,
	})
	pbVal, _ := toProtoValue(v)
	return &udalv1.SetPropertyResponse{NewValue: pbVal}, nil
}

// ─── Streaming RPC ────────────────────────────────────────────────────────────

// Subscribe streams PropertyUpdate events for a device until the client
// disconnects. If req.PropertyPath is set, only property updates for that
// exact path are forwarded; device online/offline status events (F-04 /
// #42) are always forwarded regardless, since they aren't property-scoped.
func (s *DeviceService) Subscribe(req *udalv1.SubscribeRequest, stream udalv1.DeviceService_SubscribeServer) error {
	if req.GetDeviceId() == "" {
		return status.Error(codes.InvalidArgument, "device_id is required")
	}
	d, err := s.reg.Get(req.GetDeviceId())
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return status.Errorf(codes.NotFound, "device %q not found", req.GetDeviceId())
		}
		return status.Errorf(codes.Internal, "registry get: %v", err)
	}
	if err := s.authorize(stream.Context(), auth.OpSubscribe, d); err != nil {
		return err
	}

	updates, unsubscribe := s.broker.Subscribe(req.GetDeviceId())
	defer unsubscribe()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			var resp *udalv1.SubscribeResponse
			if update.Status != nil {
				// Status events (F-04 / #42) aren't scoped to a property
				// path — always forwarded, unlike property updates below.
				resp = &udalv1.SubscribeResponse{
					DeviceId:  update.DeviceID,
					Status:    toProtoDeviceStatus(*update.Status),
					Timestamp: timestamppb.New(update.Timestamp),
				}
			} else {
				if req.GetPropertyPath() != "" && update.PropertyPath != req.GetPropertyPath() {
					continue
				}
				pbVal, err := toProtoValue(update.Value)
				if err != nil {
					continue
				}
				resp = &udalv1.SubscribeResponse{
					DeviceId:     update.DeviceID,
					PropertyPath: update.PropertyPath,
					Value:        pbVal,
					Timestamp:    timestamppb.New(update.Timestamp),
				}
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}

// ─── Command RPC ──────────────────────────────────────────────────────────────

func (s *DeviceService) SendCommand(ctx context.Context, req *udalv1.SendCommandRequest) (*udalv1.SendCommandResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if req.GetCommand() == "" {
		return nil, status.Error(codes.InvalidArgument, "command is required")
	}
	d, err := s.reg.Get(req.GetDeviceId())
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetDeviceId())
		}
		return nil, status.Errorf(codes.Internal, "registry get: %v", err)
	}
	if err := s.authorize(ctx, auth.OpSendCommand, d); err != nil {
		return nil, err
	}

	params := req.GetParams().AsMap()
	cmd := api.Command{ID: api.NewCommandID(), Name: req.GetCommand(), Params: params}

	dctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	result, err := s.commands.Dispatch(dctx, req.GetDeviceId(), cmd)
	switch {
	case errors.Is(err, api.ErrDeviceNotConnected):
		// Devices behind a transport adapter aren't wired up yet in v1.
		return nil, status.Errorf(codes.Unimplemented,
			"command %q for device %q: transport adapter not yet connected", req.GetCommand(), req.GetDeviceId())
	case errors.Is(err, context.DeadlineExceeded):
		return nil, status.Errorf(codes.DeadlineExceeded, "command %q on device %q timed out", req.GetCommand(), req.GetDeviceId())
	case err != nil:
		return nil, status.Errorf(codes.Internal, "dispatch command: %v", err)
	}
	if !result.Success {
		return nil, status.Errorf(codes.FailedPrecondition, "device %q rejected command %q: %s", req.GetDeviceId(), req.GetCommand(), result.Error)
	}

	resultValue, err := structpb.NewValue(result.Result)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode command result: %v", err)
	}
	return &udalv1.SendCommandResponse{Result: resultValue}, nil
}

// StreamCommands is opened by a device-side SDK connected directly over
// gRPC. It registers a command channel for the device named by the
// "x-device-id" metadata header, forwards each Command sent to it via
// SendCommand, and submits the device's CommandResult replies back to the
// CommandRouter for correlation.
func (s *DeviceService) StreamCommands(stream udalv1.DeviceService_StreamCommandsServer) error {
	ctx := stream.Context()
	md, _ := metadata.FromIncomingContext(ctx)
	deviceIDs := md.Get("x-device-id")
	if len(deviceIDs) == 0 || deviceIDs[0] == "" {
		return status.Error(codes.InvalidArgument, "x-device-id metadata is required")
	}
	deviceID := deviceIDs[0]

	d, err := s.reg.Get(deviceID)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return status.Errorf(codes.NotFound, "device %q not found", deviceID)
		}
		return status.Errorf(codes.Internal, "registry get: %v", err)
	}
	if err := s.authorize(ctx, auth.OpStreamCommands, d); err != nil {
		return err
	}

	commands, unregister := s.commands.Register(deviceID)
	defer unregister()

	recvErr := make(chan error, 1)
	go func() {
		for {
			res, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			s.commands.SubmitResult(api.CommandResult{
				ID:      res.GetId(),
				Success: res.GetSuccess(),
				Error:   res.GetError(),
				Result:  res.GetResult().AsInterface(),
			})
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-recvErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case cmd, ok := <-commands:
			if !ok {
				return nil
			}
			pbParams, err := structpb.NewStruct(cmd.Params)
			if err != nil {
				// Malformed params shouldn't happen (they came from a
				// well-typed google.protobuf.Struct originally) but don't
				// take down the whole stream over one bad command.
				s.commands.SubmitResult(api.CommandResult{ID: cmd.ID, Success: false, Error: fmt.Sprintf("encode params: %v", err)})
				continue
			}
			if err := stream.Send(&udalv1.Command{Id: cmd.ID, Name: cmd.Name, Params: pbParams}); err != nil {
				return err
			}
		}
	}
}

// ─── Mapping helpers ──────────────────────────────────────────────────────────

func toProtoDevice(d api.Device) *udalv1.Device {
	pb := &udalv1.Device{
		Id:         d.ID,
		Name:       d.Name,
		Capability: d.Capability,
		Transport:  d.Transport,
		Labels:     d.Labels,
		Status:     toProtoDeviceStatus(d.Status),
	}
	if !d.LastSeen.IsZero() {
		pb.LastSeen = timestamppb.New(d.LastSeen)
	}
	return pb
}

func toProtoDeviceStatus(s api.DeviceStatus) udalv1.DeviceStatus {
	switch s {
	case api.DeviceStatusOnline:
		return udalv1.DeviceStatus_DEVICE_STATUS_ONLINE
	case api.DeviceStatusOffline:
		return udalv1.DeviceStatus_DEVICE_STATUS_OFFLINE
	default:
		return udalv1.DeviceStatus_DEVICE_STATUS_UNKNOWN
	}
}

func toProtoValue(v api.PropertyValue) (*udalv1.PropertyValue, error) {
	pv := &udalv1.PropertyValue{}
	switch {
	case v.BoolVal != nil:
		pv.Value = &udalv1.PropertyValue_BoolVal{BoolVal: *v.BoolVal}
	case v.IntVal != nil:
		pv.Value = &udalv1.PropertyValue_IntVal{IntVal: *v.IntVal}
	case v.FloatVal != nil:
		pv.Value = &udalv1.PropertyValue_FloatVal{FloatVal: *v.FloatVal}
	case v.StringVal != nil:
		pv.Value = &udalv1.PropertyValue_StringVal{StringVal: *v.StringVal}
	case v.BytesVal != nil:
		pv.Value = &udalv1.PropertyValue_BytesVal{BytesVal: v.BytesVal}
	case v.JSONVal != nil:
		sv := &udalv1.PropertyValue_JsonVal{}
		// JSONVal is raw JSON; wrap in a StringValue for transport until
		// structpb unmarshalling is wired to the capability schema.
		pv.Value = sv
		_ = sv
		return nil, fmt.Errorf("JSON property values not yet supported in proto mapping")
	default:
		return nil, fmt.Errorf("empty property value")
	}
	return pv, nil
}

func fromProtoValue(pv *udalv1.PropertyValue) api.PropertyValue {
	if pv == nil {
		return api.PropertyValue{}
	}
	switch v := pv.Value.(type) {
	case *udalv1.PropertyValue_BoolVal:
		return api.BoolValue(v.BoolVal)
	case *udalv1.PropertyValue_IntVal:
		return api.IntValue(v.IntVal)
	case *udalv1.PropertyValue_FloatVal:
		return api.FloatValue(v.FloatVal)
	case *udalv1.PropertyValue_StringVal:
		return api.StringValue(v.StringVal)
	case *udalv1.PropertyValue_BytesVal:
		return api.PropertyValue{BytesVal: v.BytesVal}
	default:
		return api.PropertyValue{}
	}
}
