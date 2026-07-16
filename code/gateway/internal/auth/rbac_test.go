package auth_test

import (
	"testing"

	"github.com/paulefl/udal/code/gateway/internal/api"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func code(err error) codes.Code {
	s, _ := status.FromError(err)
	return s.Code()
}

// TestRBACMatrix exercises every role x operation cell from req42.adoc F-19.
func TestRBACMatrix(t *testing.T) {
	tests := []struct {
		role      auth.Role
		op        auth.Operation
		deviceID  string // target device; "" for device-less ops
		ownDevice string // id.DeviceID for RoleDevice cases
		wantAllow bool
	}{
		// RegisterDevice: admin/operator/device allow, reader deny. No target device.
		{auth.RoleAdmin, auth.OpRegisterDevice, "", "", true},
		{auth.RoleOperator, auth.OpRegisterDevice, "", "", true},
		{auth.RoleReader, auth.OpRegisterDevice, "", "", false},
		{auth.RoleDevice, auth.OpRegisterDevice, "", "", true},

		// GetDevice: admin/operator/reader allow; device only own.
		{auth.RoleAdmin, auth.OpGetDevice, "dev-1", "", true},
		{auth.RoleOperator, auth.OpGetDevice, "dev-1", "", true},
		{auth.RoleReader, auth.OpGetDevice, "dev-1", "", true},
		{auth.RoleDevice, auth.OpGetDevice, "dev-1", "dev-1", true},
		{auth.RoleDevice, auth.OpGetDevice, "dev-1", "dev-2", false},

		// ListDevices: admin/operator/reader allow; device deny outright.
		{auth.RoleAdmin, auth.OpListDevices, "", "", true},
		{auth.RoleOperator, auth.OpListDevices, "", "", true},
		{auth.RoleReader, auth.OpListDevices, "", "", true},
		{auth.RoleDevice, auth.OpListDevices, "", "", false},

		// DeleteDevice: admin/operator only (judgment call, not in spec table).
		{auth.RoleAdmin, auth.OpDeleteDevice, "dev-1", "", true},
		{auth.RoleOperator, auth.OpDeleteDevice, "dev-1", "", true},
		{auth.RoleReader, auth.OpDeleteDevice, "dev-1", "", false},
		{auth.RoleDevice, auth.OpDeleteDevice, "dev-1", "dev-1", false},

		// GetProperty: admin/operator/reader allow; device only own.
		{auth.RoleAdmin, auth.OpGetProperty, "dev-1", "", true},
		{auth.RoleReader, auth.OpGetProperty, "dev-1", "", true},
		{auth.RoleDevice, auth.OpGetProperty, "dev-1", "dev-1", true},
		{auth.RoleDevice, auth.OpGetProperty, "dev-1", "dev-2", false},

		// SetProperty: admin/operator allow, reader deny; device only own.
		{auth.RoleAdmin, auth.OpSetProperty, "dev-1", "", true},
		{auth.RoleOperator, auth.OpSetProperty, "dev-1", "", true},
		{auth.RoleReader, auth.OpSetProperty, "dev-1", "", false},
		{auth.RoleDevice, auth.OpSetProperty, "dev-1", "dev-1", true},
		{auth.RoleDevice, auth.OpSetProperty, "dev-1", "dev-2", false},

		// SendCommand: same shape as SetProperty.
		{auth.RoleAdmin, auth.OpSendCommand, "dev-1", "", true},
		{auth.RoleReader, auth.OpSendCommand, "dev-1", "", false},
		{auth.RoleDevice, auth.OpSendCommand, "dev-1", "dev-1", true},
		{auth.RoleDevice, auth.OpSendCommand, "dev-1", "dev-2", false},

		// Subscribe: admin/operator/reader allow; device only own.
		{auth.RoleAdmin, auth.OpSubscribe, "dev-1", "", true},
		{auth.RoleReader, auth.OpSubscribe, "dev-1", "", true},
		{auth.RoleDevice, auth.OpSubscribe, "dev-1", "dev-1", true},
		{auth.RoleDevice, auth.OpSubscribe, "dev-1", "dev-2", false},

		// StreamCommands: same shape as SendCommand.
		{auth.RoleAdmin, auth.OpStreamCommands, "dev-1", "", true},
		{auth.RoleReader, auth.OpStreamCommands, "dev-1", "", false},
		{auth.RoleDevice, auth.OpStreamCommands, "dev-1", "dev-1", true},
		{auth.RoleDevice, auth.OpStreamCommands, "dev-1", "dev-2", false},
	}

	for _, tt := range tests {
		id := auth.Identity{Subject: "s", Role: tt.role, DeviceID: tt.ownDevice}
		err := auth.Authorize(id, tt.op, tt.deviceID, nil)
		gotAllow := err == nil
		if gotAllow != tt.wantAllow {
			t.Errorf("role=%s op=%s device=%q ownDevice=%q: allow=%v, want %v (err=%v)",
				tt.role, tt.op, tt.deviceID, tt.ownDevice, gotAllow, tt.wantAllow, err)
		}
		if !tt.wantAllow && code(err) != codes.PermissionDenied {
			t.Errorf("role=%s op=%s: expected PermissionDenied, got %v", tt.role, tt.op, code(err))
		}
	}
}

func TestReaderSetPropertyDenied(t *testing.T) {
	err := auth.Authorize(auth.Identity{Subject: "r", Role: auth.RoleReader}, auth.OpSetProperty, "dev-1", nil)
	if code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", err)
	}
}

func TestDeviceOwnPropertyAllowed(t *testing.T) {
	id := auth.Identity{Subject: "dev-1", Role: auth.RoleDevice, DeviceID: "dev-1"}
	if err := auth.Authorize(id, auth.OpGetProperty, "dev-1", nil); err != nil {
		t.Errorf("expected allow, got %v", err)
	}
}

func TestDeviceForeignPropertyDenied(t *testing.T) {
	id := auth.Identity{Subject: "dev-1", Role: auth.RoleDevice, DeviceID: "dev-1"}
	err := auth.Authorize(id, auth.OpGetProperty, "dev-2", nil)
	if code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", err)
	}
}

func TestACLAllowOverridesRBACDeny(t *testing.T) {
	id := auth.Identity{Subject: "reader-1", Role: auth.RoleReader}
	acl := []api.ACLEntry{{Subject: "reader-1", Allow: true}}
	// RBAC alone would deny reader SetProperty; ACL allow should override.
	if err := auth.Authorize(id, auth.OpSetProperty, "dev-1", acl); err != nil {
		t.Errorf("expected ACL allow to override RBAC deny, got %v", err)
	}
}

func TestACLDenyOverridesRBACAllow(t *testing.T) {
	id := auth.Identity{Subject: "operator-1", Role: auth.RoleOperator}
	acl := []api.ACLEntry{{Subject: "operator-1", Allow: false}}
	// RBAC alone would allow operator SetProperty; ACL deny should override.
	err := auth.Authorize(id, auth.OpSetProperty, "dev-1", acl)
	if code(err) != codes.PermissionDenied {
		t.Errorf("expected ACL deny to override RBAC allow, got %v", err)
	}
}

func TestACLIgnoresOtherSubjects(t *testing.T) {
	id := auth.Identity{Subject: "operator-1", Role: auth.RoleOperator}
	acl := []api.ACLEntry{{Subject: "someone-else", Allow: false}}
	if err := auth.Authorize(id, auth.OpSetProperty, "dev-1", acl); err != nil {
		t.Errorf("ACL entry for a different subject should not apply, got %v", err)
	}
}
