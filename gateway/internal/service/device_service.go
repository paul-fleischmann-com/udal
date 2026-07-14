// Package service implements the gRPC DeviceService using the internal
// registry and property store.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	udalv1 "github.com/paulefl/udal/api/gen/go/udal/v1"
	"github.com/paulefl/udal/gateway/internal/api"
	"github.com/paulefl/udal/gateway/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DeviceService implements udalv1.DeviceServiceServer.
// It delegates device registration to a Registry and property storage to a
// PropertyStore. Command dispatching is forwarded to transport adapters
// (not yet wired in v1 — SendCommand returns Unimplemented).
type DeviceService struct {
	udalv1.UnimplementedDeviceServiceServer
	reg   registry.Registry
	props api.PropertyStore
}

// New returns a DeviceService backed by the given Registry and PropertyStore.
func New(reg registry.Registry, props api.PropertyStore) *DeviceService {
	return &DeviceService{reg: reg, props: props}
}

// ─── Device registry RPCs ─────────────────────────────────────────────────────

func (s *DeviceService) GetDevice(_ context.Context, req *udalv1.GetDeviceRequest) (*udalv1.GetDeviceResponse, error) {
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
	return &udalv1.GetDeviceResponse{Device: toProtoDevice(d)}, nil
}

func (s *DeviceService) ListDevices(_ context.Context, req *udalv1.ListDevicesRequest) (*udalv1.ListDevicesResponse, error) {
	devices, err := s.reg.List(req.GetCapability(), req.GetTransport())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "registry list: %v", err)
	}
	pb := make([]*udalv1.Device, 0, len(devices))
	for _, d := range devices {
		pb = append(pb, toProtoDevice(d))
	}
	return &udalv1.ListDevicesResponse{Devices: pb}, nil
}

func (s *DeviceService) RegisterDevice(_ context.Context, req *udalv1.RegisterDeviceRequest) (*udalv1.RegisterDeviceResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.GetCapability() == "" {
		return nil, status.Error(codes.InvalidArgument, "capability is required")
	}
	if req.GetTransport() == "" {
		return nil, status.Error(codes.InvalidArgument, "transport is required")
	}
	d, err := s.reg.Register(api.Device{
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
	return &udalv1.RegisterDeviceResponse{Device: toProtoDevice(d)}, nil
}

func (s *DeviceService) DeleteDevice(_ context.Context, req *udalv1.DeleteDeviceRequest) (*udalv1.DeleteDeviceResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
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

func (s *DeviceService) GetProperty(_ context.Context, req *udalv1.GetPropertyRequest) (*udalv1.GetPropertyResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if req.GetPropertyPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "property_path is required")
	}
	if _, err := s.reg.Get(req.GetDeviceId()); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetDeviceId())
		}
		return nil, status.Errorf(codes.Internal, "registry get: %v", err)
	}
	v, err := s.props.Get(req.GetDeviceId(), req.GetPropertyPath())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "property %q not found on device %q", req.GetPropertyPath(), req.GetDeviceId())
	}
	pbVal, err := toProtoValue(v)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode property value: %v", err)
	}
	return &udalv1.GetPropertyResponse{Value: pbVal}, nil
}

func (s *DeviceService) SetProperty(_ context.Context, req *udalv1.SetPropertyRequest) (*udalv1.SetPropertyResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if req.GetPropertyPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "property_path is required")
	}
	if _, err := s.reg.Get(req.GetDeviceId()); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetDeviceId())
		}
		return nil, status.Errorf(codes.Internal, "registry get: %v", err)
	}
	v := fromProtoValue(req.GetValue())
	if err := s.props.Set(req.GetDeviceId(), req.GetPropertyPath(), v); err != nil {
		return nil, status.Errorf(codes.Internal, "set property: %v", err)
	}
	pbVal, _ := toProtoValue(v)
	_ = s.reg.UpdateStatus(req.GetDeviceId(), api.DeviceStatusOnline, time.Now())
	return &udalv1.SetPropertyResponse{NewValue: pbVal}, nil
}

// ─── Command RPC ──────────────────────────────────────────────────────────────

func (s *DeviceService) SendCommand(_ context.Context, req *udalv1.SendCommandRequest) (*udalv1.SendCommandResponse, error) {
	if req.GetDeviceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if req.GetCommand() == "" {
		return nil, status.Error(codes.InvalidArgument, "command is required")
	}
	if _, err := s.reg.Get(req.GetDeviceId()); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetDeviceId())
		}
		return nil, status.Errorf(codes.Internal, "registry get: %v", err)
	}
	// Command dispatching is handled by the transport adapter layer.
	// For now, return Unimplemented so the caller knows routing is pending.
	return nil, status.Errorf(codes.Unimplemented,
		"command %q for device %q: transport adapter not yet connected", req.GetCommand(), req.GetDeviceId())
}

// ─── Mapping helpers ──────────────────────────────────────────────────────────

func toProtoDevice(d api.Device) *udalv1.Device {
	pb := &udalv1.Device{
		Id:         d.ID,
		Name:       d.Name,
		Capability: d.Capability,
		Transport:  d.Transport,
		Labels:     d.Labels,
	}
	switch d.Status {
	case api.DeviceStatusOnline:
		pb.Status = udalv1.DeviceStatus_DEVICE_STATUS_ONLINE
	case api.DeviceStatusOffline:
		pb.Status = udalv1.DeviceStatus_DEVICE_STATUS_OFFLINE
	default:
		pb.Status = udalv1.DeviceStatus_DEVICE_STATUS_UNKNOWN
	}
	if !d.LastSeen.IsZero() {
		pb.LastSeen = timestamppb.New(d.LastSeen)
	}
	return pb
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
