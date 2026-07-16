package udal

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Error is returned by every SDK operation that fails. Code mirrors the
// gRPC status code the gateway responded with (see
// google.golang.org/grpc/codes), letting callers distinguish e.g. NotFound
// from PermissionDenied without depending on grpc-go directly.
type Error struct {
	Code    codes.Code
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("udal: %s: %s", e.Code, e.Message)
}

// wrapError converts a gRPC (or any other) error into *Error. Returns nil
// for a nil input.
func wrapError(err error) error {
	if err == nil {
		return nil
	}
	s, ok := status.FromError(err)
	if !ok {
		return &Error{Code: codes.Unknown, Message: err.Error()}
	}
	return &Error{Code: s.Code(), Message: s.Message()}
}
