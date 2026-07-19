// Package service implements the gRPC DeviceService using the internal
// registry and property store.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/adapter"
	canadapter "github.com/paulefl/udal/code/gateway/internal/adapters/can"
	httpadapter "github.com/paulefl/udal/code/gateway/internal/adapters/http"
	mqttadapter "github.com/paulefl/udal/code/gateway/internal/adapters/mqtt"
	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/capability"
	"github.com/paulefl/udal/code/gateway/internal/metrics"
	"github.com/paulefl/udal/code/gateway/internal/registry"
	"github.com/paulefl/udal/code/gateway/internal/tracing"
	"go.opentelemetry.io/otel"
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

// HTTPAdapter is the subset of *httpadapter.Adapter's API DeviceService
// needs to route GetProperty for http-transport devices (issue #24). Takes
// the full api.Device rather than just its ID (unlike MQTTAdapter) because,
// unlike MQTT's broker-relative topic convention, an HTTP device's base URL
// isn't derivable from its ID alone — it lives in Device.Labels. No
// WriteProperty: issue #24's acceptance criteria don't include one (see
// package httpadapter's doc comment), so SetProperty for http-transport
// devices still falls through to the in-memory PropertyStore, same as any
// transport without an adapter configured.
type HTTPAdapter interface {
	ReadProperty(ctx context.Context, d api.Device, path string) (api.PropertyValue, error)
	WatchDevice(ctx context.Context, d api.Device) error
}

// CANAdapter is the subset of *canadapter.Adapter's API DeviceService needs
// to route GetProperty/SetProperty for can-transport devices (issue #25).
// Takes the full api.Device, like HTTPAdapter and unlike MQTTAdapter:
// resolving a device to its DBC message needs Device.Labels (see
// canadapter.LabelMessage), the CAN-bus equivalent of HTTP's per-device
// endpoint label — a device's ID alone doesn't say which DBC message its
// properties live in, since one DBC file/bus is shared by every device on
// it. Unlike HTTPAdapter, WriteProperty is included: issue #25's
// acceptance criteria explicitly list one ("WriteProperty: encodes value to
// CAN frame, writes to SocketCAN interface"), unlike #24's HTTP adapter.
type CANAdapter interface {
	ReadProperty(ctx context.Context, d api.Device, path string) (api.PropertyValue, error)
	WriteProperty(ctx context.Context, d api.Device, path string, v api.PropertyValue) error
	WatchDevice(ctx context.Context, d api.Device) error
}

// PresenceMonitor is the subset of *heartbeat.Monitor's API DeviceService
// needs (issue #42): Touch marks a device alive right now. RegisterDevice
// touches on (re-)registration; StreamCommands touches once on connect and
// then on every tick of Interval for as long as the stream stays open —
// treating an open direct-gRPC connection as its own continuous heartbeat,
// since there's no per-message heartbeat protocol for that transport.
type PresenceMonitor interface {
	Touch(deviceID string) error
	Interval() time.Duration
}

// CapabilityRegistry is the subset of *capability.Registry's API
// DeviceService needs (issue #22): resolving a device's declared capability
// ("name@version", see RegisterDevice) to validate it exists (F-14) and to
// validate SetProperty values against its declared property types (F-15).
type CapabilityRegistry interface {
	Get(name, version string) (capability.Schema, error)
}

// DeviceService implements udalv1.DeviceServiceServer.
// It delegates device registration to a Registry; property storage goes to
// a PropertyStore for most devices, to an MQTTAdapter for devices whose
// Transport is "mqtt" (if one is configured — see SetMQTTAdapter), to an
// HTTPAdapter for devices whose Transport is "http" (if one is configured —
// see SetHTTPAdapter; GetProperty only, see HTTPAdapter's doc comment), or
// to a CANAdapter for devices whose Transport is "can" (if one is
// configured — see SetCANAdapter). SendCommand is routed to a
// directly-connected gRPC device's StreamCommands channel via commands, if
// one is open for that device; SendCommand-over-MQTT/HTTP/CAN isn't in
// scope (no acceptance criterion requires it for any of the three), so it
// still returns Unimplemented for those devices.
type DeviceService struct {
	udalv1.UnimplementedDeviceServiceServer
	reg        registry.Registry
	props      api.PropertyStore
	broker     *api.Broker
	commands   *api.CommandRouter
	mqtt       MQTTAdapter
	http       HTTPAdapter
	can        CANAdapter
	custom     map[string]adapter.Transport
	presence   PresenceMonitor
	capability CapabilityRegistry
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

// SetHTTPAdapter wires an HTTPAdapter into the service so http-transport
// devices' GetProperty routes through it instead of the in-memory
// PropertyStore. Optional — a DeviceService without one behaves exactly as
// before (http-transport devices fall through to PropertyStore, same as
// any other transport).
func (s *DeviceService) SetHTTPAdapter(a HTTPAdapter) { s.http = a }

// SetCANAdapter wires a CANAdapter into the service so can-transport
// devices' GetProperty/SetProperty route through it instead of the
// in-memory PropertyStore. Optional — a DeviceService without one behaves
// exactly as before (can-transport devices fall through to PropertyStore,
// same as any other transport).
func (s *DeviceService) SetCANAdapter(a CANAdapter) { s.can = a }

// SetCustomTransports wires third-party adapter.Transport implementations
// into the service (req42.adoc F-12, issue #26) — one entry per activated
// name (see adapter.Register/Lookup and cmd/gateway/main.go's
// adapters.custom wiring). A device whose Transport field matches a key in
// transports routes GetProperty/SetProperty through it, exactly like the
// three built-in adapters above; a device whose Transport matches no key
// here (and isn't "mqtt"/"http"/"can") falls through to the in-memory
// PropertyStore, unchanged from before this feature existed. Optional — a
// DeviceService without any custom transports set behaves exactly as
// before.
func (s *DeviceService) SetCustomTransports(transports map[string]adapter.Transport) {
	s.custom = transports
}

// SetPresenceMonitor wires a PresenceMonitor into the service so
// RegisterDevice and StreamCommands report device liveness to it (issue
// #42). Optional — without one, neither touches anything (no behavior
// change from before this feature existed).
func (s *DeviceService) SetPresenceMonitor(m PresenceMonitor) { s.presence = m }

// SetCapabilityRegistry wires a CapabilityRegistry into the service so
// RegisterDevice validates the declared capability schema exists (F-14) and
// SetProperty validates values against it (F-15). Optional — a
// DeviceService without one behaves exactly as before (no schema
// enforcement at all), so existing devices/tests that register with an
// arbitrary Capability string are unaffected unless this is configured.
func (s *DeviceService) SetCapabilityRegistry(r CapabilityRegistry) { s.capability = r }

// splitCapabilityRef splits a "name@version" capability reference. Devices
// declare their capability this way once a CapabilityRegistry is
// configured (see req42.adoc F-13: schemas are "retrievable by
// name@version").
func splitCapabilityRef(ref string) (name, version string, ok bool) {
	name, version, ok = strings.Cut(ref, "@")
	if !ok || name == "" || version == "" {
		return "", "", false
	}
	return name, version, true
}

// capabilityRefStatusError maps a capability.Registry lookup/validation
// error (from RegisterDevice/SetProperty resolving a device's declared
// schema) to a gRPC status. Distinct from CapabilityService's own
// capabilityStatusError (in capability_service.go, for its Publish/Get/List
// RPCs) despite the overlapping error set — kept separate since the two
// callers reach these errors through different code paths and one extra
// case here (ErrPropertyNotDeclared) doesn't apply to the other at all.
func capabilityRefStatusError(err error) error {
	switch {
	case errors.Is(err, capability.ErrNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, capability.ErrPropertyNotDeclared):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, capability.ErrInvalidPropertyValue):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}

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

// httpStatusError maps an HTTPAdapter error to a gRPC status (req42.adoc
// F-10: "HTTP errors (4xx/5xx) mapped to appropriate gRPC status codes").
// A *httpadapter.StatusError carries the device's actual HTTP response
// status; anything else (network failure, malformed body, missing
// endpoint label) is a gateway-side/connectivity problem, not a device
// response to translate, so it maps to Internal like mqttStatusError's
// default case.
func httpStatusError(err error) error {
	var se *httpadapter.StatusError
	if errors.As(err, &se) {
		switch {
		case se.StatusCode == 404:
			return status.Errorf(codes.NotFound, "%v", err)
		case se.StatusCode == 401:
			return status.Errorf(codes.Unauthenticated, "%v", err)
		case se.StatusCode == 403:
			return status.Errorf(codes.PermissionDenied, "%v", err)
		case se.StatusCode == 408:
			return status.Errorf(codes.DeadlineExceeded, "%v", err)
		case se.StatusCode == 429:
			return status.Errorf(codes.ResourceExhausted, "%v", err)
		case se.StatusCode >= 500:
			return status.Errorf(codes.Unavailable, "%v", err)
		default: // remaining 4xx
			return status.Errorf(codes.InvalidArgument, "%v", err)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Errorf(codes.DeadlineExceeded, "%v", err)
	}
	return status.Errorf(codes.Internal, "%v", err)
}

// canStatusError maps a CANAdapter error to a gRPC status. Unknown-message/
// unknown-signal/no-frame-yet are the CAN equivalent of "not found" (there
// is no such addressable thing, or nothing has arrived for it yet); a
// missing can.message label is a caller/config problem, so it maps like a
// malformed request rather than a missing resource.
func canStatusError(err error) error {
	switch {
	case errors.Is(err, canadapter.ErrUnknownMessage),
		errors.Is(err, canadapter.ErrUnknownSignal),
		errors.Is(err, canadapter.ErrNoFrameYet):
		return status.Errorf(codes.NotFound, "can: %v", err)
	case errors.Is(err, canadapter.ErrMissingLabel):
		return status.Errorf(codes.InvalidArgument, "can: %v", err)
	case errors.Is(err, canadapter.ErrNotOpen), errors.Is(err, canadapter.ErrLinuxOnly):
		return status.Errorf(codes.Unavailable, "can: %v", err)
	case errors.Is(err, context.DeadlineExceeded):
		return status.Errorf(codes.DeadlineExceeded, "can: %v", err)
	default:
		return status.Errorf(codes.Internal, "can: %v", err)
	}
}

// customStatusError maps a third-party adapter.Transport error to a gRPC
// status (issue #26). Unlike mqttStatusError/httpStatusError/
// canStatusError, DeviceService has no knowledge of a third-party
// Transport's internal error types to switch on — the two cases it can
// recognize generically are adapter.ErrWriteNotSupported (from the
// interface itself, see SetProperty) and context.DeadlineExceeded (from
// the ctx SetProperty/GetProperty already passed in); anything else maps
// to Internal, same as the built-in adapters' own default case.
func customStatusError(name string, err error) error {
	switch {
	case errors.Is(err, adapter.ErrWriteNotSupported):
		return status.Errorf(codes.Unimplemented, "%s: %v", name, err)
	case errors.Is(err, context.DeadlineExceeded):
		return status.Errorf(codes.DeadlineExceeded, "%s: %v", name, err)
	default:
		return status.Errorf(codes.Internal, "%s: %v", name, err)
	}
}

// commandTimeout is F-07's "Command timeout (configurable, default 10 s)".
// Not yet wired to gateway config (#41 YAML config isn't done); hardcoded
// default for now.
const commandTimeout = 10 * time.Second

// ─── Tracing helper ───────────────────────────────────────────────────────────

// startSpan starts a span named name as a child of ctx's active span
// (req42.adoc F-24, issue #29) and returns a matching end func the caller
// must invoke exactly once, with the operation's error (nil for success).
// Used for GetProperty/SetProperty's "router" and "adapter" spans — the
// only two RPCs that actually dispatch to a transport adapter, unlike
// tracing.Interceptor's "api" span and auth.Authenticator's "auth" span,
// which every RPC gets.
func startSpan(ctx context.Context, name string) (context.Context, func(err error)) {
	spanCtx, span := otel.Tracer(tracing.TracerName).Start(ctx, name)
	return spanCtx, func(err error) {
		tracing.RecordError(span, err)
		span.End()
	}
}

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
	if s.capability != nil {
		// F-14: reject registration if the declared capability schema
		// doesn't exist. Devices reference a schema as "name@version" once
		// a CapabilityRegistry is configured (see SetCapabilityRegistry).
		name, version, ok := splitCapabilityRef(req.GetCapability())
		if !ok {
			return nil, status.Errorf(codes.NotFound, "capability %q is not a valid name@version reference", req.GetCapability())
		}
		if _, err := s.capability.Get(name, version); err != nil {
			return nil, capabilityRefStatusError(err)
		}
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
	if d.Transport == "http" && s.http != nil {
		// Best-effort, same reasoning as the mqtt branch above: a failure
		// here (e.g. missing http.endpoint label) only means this device's
		// poll loop/webhook registration didn't start — ReadProperty still
		// surfaces that same error clearly on the next GetProperty call.
		_ = s.http.WatchDevice(ctx, d)
	}
	if d.Transport == "can" && s.can != nil {
		// Best-effort, same reasoning as the http branch above: a failure
		// here (e.g. missing can.message label, or the label naming a
		// message not in the loaded DBC file) only means this device isn't
		// registered for OnPropertyUpdate fan-out yet — ReadProperty/
		// WriteProperty still surface that same error clearly on the next
		// call.
		_ = s.can.WatchDevice(ctx, d)
	}
	if t, ok := s.custom[d.Transport]; ok {
		// Best-effort, same reasoning as the http/can branches above.
		_ = t.WatchDevice(ctx, d)
	}
	if s.presence != nil {
		// Marks the device online immediately on (re-)registration —
		// covers "device reconnects -> online=true" (#42 AC3) for the
		// case of a device re-registering after a gateway/process
		// restart, without waiting for its next heartbeat. Re-fetch so
		// the response reflects the just-touched status rather than the
		// pre-touch snapshot from Register.
		if err := s.presence.Touch(d.ID); err == nil {
			if touched, err := s.reg.Get(d.ID); err == nil {
				d = touched
			}
		}
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

// resp/err are named so the deferred endRouterSpan(err) below sees the
// error of *every* exit path (including the PropertyStore fallback and the
// value-encode step, not just the three adapter branches) — a plain local
// routeErr variable that only the adapter branches assigned was tried
// first and found to leave the "router" span reporting success on request
// paths that actually failed (code review finding, issue #29).
func (s *DeviceService) GetProperty(ctx context.Context, req *udalv1.GetPropertyRequest) (resp *udalv1.GetPropertyResponse, err error) {
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
	custom, isCustom := s.custom[d.Transport]
	routerCtx, endRouterSpan := startSpan(ctx, "router")
	defer func() { endRouterSpan(err) }()

	switch {
	case d.Transport == "mqtt" && s.mqtt != nil:
		adapterCtx, endAdapterSpan := startSpan(routerCtx, "adapter")
		v, err = s.mqtt.ReadProperty(adapterCtx, req.GetDeviceId(), req.GetPropertyPath())
		endAdapterSpan(err)
		if err != nil {
			metrics.AdapterErrors.WithLabelValues("mqtt_adapter").Inc()
			return nil, mqttStatusError(err)
		}
	case d.Transport == "http" && s.http != nil:
		adapterCtx, endAdapterSpan := startSpan(routerCtx, "adapter")
		v, err = s.http.ReadProperty(adapterCtx, d, req.GetPropertyPath())
		endAdapterSpan(err)
		if err != nil {
			metrics.AdapterErrors.WithLabelValues("http_adapter").Inc()
			return nil, httpStatusError(err)
		}
	case d.Transport == "can" && s.can != nil:
		adapterCtx, endAdapterSpan := startSpan(routerCtx, "adapter")
		v, err = s.can.ReadProperty(adapterCtx, d, req.GetPropertyPath())
		endAdapterSpan(err)
		if err != nil {
			metrics.AdapterErrors.WithLabelValues("can_adapter").Inc()
			return nil, canStatusError(err)
		}
	case isCustom:
		adapterCtx, endAdapterSpan := startSpan(routerCtx, "adapter")
		v, err = custom.ReadProperty(adapterCtx, d, req.GetPropertyPath())
		endAdapterSpan(err)
		if err != nil {
			metrics.AdapterErrors.WithLabelValues(custom.Name()).Inc()
			return nil, customStatusError(custom.Name(), err)
		}
	default:
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

// resp/err are named for the same reason as GetProperty's — see its doc
// comment.
func (s *DeviceService) SetProperty(ctx context.Context, req *udalv1.SetPropertyRequest) (resp *udalv1.SetPropertyResponse, err error) {
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

	if s.capability != nil {
		// F-15: validate the value against the device's declared schema
		// before forwarding to the adapter/PropertyStore. If d.Capability
		// isn't a valid name@version reference (only possible for a device
		// registered before a CapabilityRegistry was configured — a
		// currently-configured registry always enforces the format at
		// RegisterDevice time), there's no schema to validate against, so
		// skip rather than block an otherwise-legitimate write.
		if name, version, ok := splitCapabilityRef(d.Capability); ok {
			schema, err := s.capability.Get(name, version)
			if err != nil {
				return nil, capabilityRefStatusError(err)
			}
			if err := schema.ValidateProperty(req.GetPropertyPath(), v); err != nil {
				return nil, capabilityRefStatusError(err)
			}
		}
	}

	custom, isCustom := s.custom[d.Transport]
	routerCtx, endRouterSpan := startSpan(ctx, "router")
	defer func() { endRouterSpan(err) }()

	if d.Transport == "http" && s.http != nil {
		// Explicit Unimplemented rather than silently falling through to
		// PropertyStore below: once an HTTPAdapter is configured,
		// GetProperty for this device always polls it live (see
		// GetProperty's switch) — a silent PropertyStore write here would
		// be invisible to every subsequent read, which is worse than
		// reporting plainly that writes aren't supported for this
		// transport (issue #24's AC list has no WriteProperty, see
		// package httpadapter's doc comment).
		return nil, status.Errorf(codes.Unimplemented,
			"device %q: SetProperty over HTTP is not supported", req.GetDeviceId())
	}

	if d.Transport == "mqtt" && s.mqtt != nil {
		adapterCtx, endAdapterSpan := startSpan(routerCtx, "adapter")
		err = s.mqtt.WriteProperty(adapterCtx, req.GetDeviceId(), req.GetPropertyPath(), v)
		endAdapterSpan(err)
		if err != nil {
			metrics.AdapterErrors.WithLabelValues("mqtt_adapter").Inc()
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

	if d.Transport == "can" && s.can != nil {
		adapterCtx, endAdapterSpan := startSpan(routerCtx, "adapter")
		err = s.can.WriteProperty(adapterCtx, d, req.GetPropertyPath(), v)
		endAdapterSpan(err)
		if err != nil {
			metrics.AdapterErrors.WithLabelValues("can_adapter").Inc()
			return nil, canStatusError(err)
		}
		// No broker.Publish here, same reasoning as the mqtt branch above:
		// WriteProperty already updates the adapter's own cache and fires
		// OnPropertyUpdate (wired up in main.go) for the frame it just
		// wrote, so Subscribe fan-out happens through that path, not here.
		_ = s.reg.UpdateStatus(req.GetDeviceId(), api.DeviceStatusOnline, time.Now())
		pbVal, _ := toProtoValue(v)
		return &udalv1.SetPropertyResponse{NewValue: pbVal}, nil
	}

	if isCustom {
		adapterCtx, endAdapterSpan := startSpan(routerCtx, "adapter")
		err = custom.WriteProperty(adapterCtx, d, req.GetPropertyPath(), v)
		endAdapterSpan(err)
		if err != nil {
			metrics.AdapterErrors.WithLabelValues(custom.Name()).Inc()
			return nil, customStatusError(custom.Name(), err)
		}
		// Unlike the built-in mqtt/can branches above, a third-party
		// Transport has no OnPropertyUpdate-style callback wired up in
		// main.go to drive Subscribe fan-out on its own (adapter.Transport
		// has no such hook, issue #26) — so this call publishes directly,
		// same as the PropertyStore fallback below.
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

	// An open StreamCommands connection is itself a continuous liveness
	// signal (#42): touch presence now and on every tick thereafter, for
	// as long as the stream stays open, so a device with no in-flight
	// commands doesn't get incorrectly timed out. heartbeatTick stays nil
	// (blocks forever, never selected below) if no PresenceMonitor is
	// configured, or if one is but reports a non-positive interval --
	// time.NewTicker panics on <= 0, and a single misbehaving
	// PresenceMonitor implementation shouldn't be able to crash every
	// connected device's StreamCommands handler.
	var heartbeatTick <-chan time.Time
	if s.presence != nil {
		_ = s.presence.Touch(deviceID)
		if interval := s.presence.Interval(); interval > 0 {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			heartbeatTick = ticker.C
		}
	}

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
		case <-heartbeatTick:
			_ = s.presence.Touch(deviceID)
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
