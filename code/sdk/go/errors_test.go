package udal

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWrapError_Nil(t *testing.T) {
	if wrapError(nil) != nil {
		t.Error("expected nil")
	}
}

func TestWrapError_GRPCStatus(t *testing.T) {
	err := wrapError(status.Error(codes.NotFound, "device not found"))
	var udalErr *Error
	if !errors.As(err, &udalErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if udalErr.Code != codes.NotFound || udalErr.Message != "device not found" {
		t.Errorf("unexpected *Error: %+v", udalErr)
	}
}

func TestWrapError_NonGRPC(t *testing.T) {
	err := wrapError(errors.New("plain error"))
	var udalErr *Error
	if !errors.As(err, &udalErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if udalErr.Code != codes.Unknown {
		t.Errorf("Code = %v, want Unknown", udalErr.Code)
	}
}
