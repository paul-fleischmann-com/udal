// Package auth implements authentication (API-Key, mTLS, JWT Bearer) and
// authorization (RBAC + per-device ACL) for the gRPC DeviceService.
package auth

import (
	"github.com/paulefl/udal/code/gateway/internal/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Role is the authorization role of an authenticated caller.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleReader   Role = "reader"
	RoleDevice   Role = "device"
)

// Identity is the authenticated caller, resolved by one of the AuthN methods
// (API-Key, mTLS, JWT) before RBAC/ACL authorization runs.
type Identity struct {
	// Subject identifies the caller: the API key's owner, the mTLS
	// certificate's CN, or the JWT's `sub` claim.
	Subject string
	Role    Role
	// DeviceID is set only for RoleDevice identities; it scopes "own device"
	// operations (see rbac table below) to the device the credential belongs
	// to. By convention it equals the mTLS certificate's CN.
	DeviceID string
}

// Operation identifies a DeviceService RPC for RBAC purposes.
type Operation string

const (
	OpRegisterDevice Operation = "RegisterDevice"
	OpGetDevice      Operation = "GetDevice"
	OpListDevices    Operation = "ListDevices"
	OpDeleteDevice   Operation = "DeleteDevice"
	OpGetProperty    Operation = "GetProperty"
	OpSetProperty    Operation = "SetProperty"
	OpSendCommand    Operation = "SendCommand"
	OpSubscribe      Operation = "Subscribe"
	// OpStreamCommands isn't in F-19's table either (it's an implementation
	// detail of how a directly-connected gRPC device receives commands, see
	// the #12 plan doc); treated the same as SendCommand/SetProperty — a
	// device may only open its own command stream.
	OpStreamCommands Operation = "StreamCommands"

	// OpPublishSchema/OpGetSchema/OpListSchemas (CapabilityService, #22)
	// aren't in F-19's table either, which predates this service — a
	// documented judgment call, same as OpDeleteDevice above. Publishing a
	// schema is treated like RegisterDevice minus the device role (a
	// device registers itself, but doesn't define capability schemas for
	// the system); reading schemas is treated like GetDevice/ListDevices
	// (any authenticated non-device caller), since a device references its
	// own capability by name at registration time rather than fetching the
	// schema's content through this API.
	OpPublishSchema Operation = "PublishSchema"
	OpGetSchema     Operation = "GetSchema"
	OpListSchemas   Operation = "ListSchemas"
)

// permission describes what a caller in a given role may do for an
// operation: never, always, or only when it targets its own device.
type permission int

const (
	deny permission = iota
	allow
	ownOnly
)

// rbac is the role x operation matrix from req42.adoc F-19. DeleteDevice is
// not part of the spec's table; it is treated like RegisterDevice
// (admin/operator only) as a documented judgment call — see the plan doc for
// this issue.
var rbac = map[Operation]map[Role]permission{
	OpRegisterDevice: {RoleAdmin: allow, RoleOperator: allow, RoleReader: deny, RoleDevice: allow},
	OpGetDevice:      {RoleAdmin: allow, RoleOperator: allow, RoleReader: allow, RoleDevice: ownOnly},
	OpListDevices:    {RoleAdmin: allow, RoleOperator: allow, RoleReader: allow, RoleDevice: deny},
	OpDeleteDevice:   {RoleAdmin: allow, RoleOperator: allow, RoleReader: deny, RoleDevice: deny},
	OpGetProperty:    {RoleAdmin: allow, RoleOperator: allow, RoleReader: allow, RoleDevice: ownOnly},
	OpSetProperty:    {RoleAdmin: allow, RoleOperator: allow, RoleReader: deny, RoleDevice: ownOnly},
	OpSendCommand:    {RoleAdmin: allow, RoleOperator: allow, RoleReader: deny, RoleDevice: ownOnly},
	OpSubscribe:      {RoleAdmin: allow, RoleOperator: allow, RoleReader: allow, RoleDevice: ownOnly},
	OpStreamCommands: {RoleAdmin: allow, RoleOperator: allow, RoleReader: deny, RoleDevice: ownOnly},
	OpPublishSchema:  {RoleAdmin: allow, RoleOperator: allow, RoleReader: deny, RoleDevice: deny},
	OpGetSchema:      {RoleAdmin: allow, RoleOperator: allow, RoleReader: allow, RoleDevice: deny},
	OpListSchemas:    {RoleAdmin: allow, RoleOperator: allow, RoleReader: allow, RoleDevice: deny},
}

// Authorize decides whether id may perform op against the device identified
// by deviceID ("" for operations with no single target device, e.g.
// ListDevices or RegisterDevice). acl is the target device's ACL entries;
// pass nil when deviceID is "".
//
// Per F-20, an ACL entry for id.Subject overrides the RBAC decision in
// either direction: an allow entry permits a caller RBAC would deny, and a
// deny entry blocks a caller RBAC would allow. Absent a matching ACL entry,
// the RBAC decision stands.
func Authorize(id Identity, op Operation, deviceID string, acl []api.ACLEntry) error {
	for _, entry := range acl {
		if entry.Subject != id.Subject {
			continue
		}
		if entry.Allow {
			return nil
		}
		return permissionDeniedError(id, op, deviceID)
	}

	if rbacAllows(id, op, deviceID) {
		return nil
	}
	return permissionDeniedError(id, op, deviceID)
}

func rbacAllows(id Identity, op Operation, deviceID string) bool {
	perms, ok := rbac[op]
	if !ok {
		return false
	}
	switch perms[id.Role] {
	case allow:
		return true
	case ownOnly:
		return deviceID != "" && id.DeviceID == deviceID
	default:
		return false
	}
}

func permissionDeniedError(id Identity, op Operation, deviceID string) error {
	if deviceID == "" {
		return status.Errorf(codes.PermissionDenied, "role %q may not call %s", id.Role, op)
	}
	return status.Errorf(codes.PermissionDenied, "role %q may not call %s on device %q", id.Role, op, deviceID)
}
