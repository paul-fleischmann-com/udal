package service_test

import (
	"context"
	"testing"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/capability"
	"github.com/paulefl/udal/code/gateway/internal/service"
	"google.golang.org/grpc/codes"
)

func readerContext() context.Context {
	return auth.ContextWithIdentity(context.Background(), auth.Identity{Subject: "test-reader", Role: auth.RoleReader})
}

const testCapabilityDoc = `{
	"udal": "1.0",
	"kind": "DeviceCapability",
	"metadata": {"name": "widget", "version": "1.0.0"},
	"properties": {"level": {"type": "int"}}
}`

func TestCapabilityService_PublishGetList(t *testing.T) {
	svc := service.NewCapabilityService(capability.NewMemoryRegistry())

	publishResp, err := svc.PublishSchema(adminCtx(), &udalv1.PublishSchemaRequest{Schema: []byte(testCapabilityDoc)})
	if err != nil {
		t.Fatalf("PublishSchema: %v", err)
	}
	if publishResp.GetSchema().GetName() != "widget" || publishResp.GetSchema().GetVersion() != "1.0.0" {
		t.Errorf("PublishSchema response = %+v", publishResp.GetSchema())
	}
	if publishResp.GetSchema().GetPublishedAt() == nil {
		t.Error("PublishSchema response has no published_at timestamp")
	}

	getResp, err := svc.GetSchema(adminCtx(), &udalv1.GetSchemaRequest{Name: "widget", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("GetSchema: %v", err)
	}
	if string(getResp.GetSchema().GetRaw()) != testCapabilityDoc {
		t.Errorf("GetSchema raw mismatch")
	}

	listResp, err := svc.ListSchemas(adminCtx(), &udalv1.ListSchemasRequest{})
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	if len(listResp.GetSchemas()) != 1 || listResp.GetSchemas()[0].GetName() != "widget" {
		t.Errorf("ListSchemas = %+v", listResp.GetSchemas())
	}
}

func TestCapabilityService_PublishDuplicateReturnsAlreadyExists(t *testing.T) {
	svc := service.NewCapabilityService(capability.NewMemoryRegistry())
	req := &udalv1.PublishSchemaRequest{Schema: []byte(testCapabilityDoc)}
	if _, err := svc.PublishSchema(adminCtx(), req); err != nil {
		t.Fatalf("first PublishSchema: %v", err)
	}
	_, err := svc.PublishSchema(adminCtx(), req)
	if grpcCode(err) != codes.AlreadyExists {
		t.Errorf("second PublishSchema = %v, want AlreadyExists", err)
	}
}

func TestCapabilityService_PublishInvalidSchemaReturnsInvalidArgument(t *testing.T) {
	svc := service.NewCapabilityService(capability.NewMemoryRegistry())
	_, err := svc.PublishSchema(adminCtx(), &udalv1.PublishSchemaRequest{Schema: []byte(`{"udal": "1.0"}`)})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("PublishSchema(invalid) = %v, want InvalidArgument", err)
	}
}

func TestCapabilityService_PublishEmptySchemaReturnsInvalidArgument(t *testing.T) {
	svc := service.NewCapabilityService(capability.NewMemoryRegistry())
	_, err := svc.PublishSchema(adminCtx(), &udalv1.PublishSchemaRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("PublishSchema(empty) = %v, want InvalidArgument", err)
	}
}

func TestCapabilityService_GetUnknownReturnsNotFound(t *testing.T) {
	svc := service.NewCapabilityService(capability.NewMemoryRegistry())
	_, err := svc.GetSchema(adminCtx(), &udalv1.GetSchemaRequest{Name: "missing", Version: "1.0.0"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("GetSchema(missing) = %v, want NotFound", err)
	}
}

func TestCapabilityService_GetEmptyArgsReturnsInvalidArgument(t *testing.T) {
	svc := service.NewCapabilityService(capability.NewMemoryRegistry())
	_, err := svc.GetSchema(adminCtx(), &udalv1.GetSchemaRequest{Name: "widget"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("GetSchema(no version) = %v, want InvalidArgument", err)
	}
}

func TestCapabilityService_ReaderCannotPublish(t *testing.T) {
	svc := service.NewCapabilityService(capability.NewMemoryRegistry())
	readerCtx := readerContext()
	_, err := svc.PublishSchema(readerCtx, &udalv1.PublishSchemaRequest{Schema: []byte(testCapabilityDoc)})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("reader PublishSchema = %v, want PermissionDenied", err)
	}
}
