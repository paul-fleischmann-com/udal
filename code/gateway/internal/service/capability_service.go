package service

import (
	"context"
	"errors"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"github.com/paulefl/udal/code/gateway/internal/auth"
	"github.com/paulefl/udal/code/gateway/internal/capability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CapabilityService implements udalv1.CapabilityServiceServer (F-13):
// publishes, retrieves, and lists capability schemas. DeviceService
// separately enforces F-14/F-15 against whatever registry is wired into it
// via SetCapabilityRegistry — the two share the same underlying
// capability.Registry in main.go, but this service is the direct
// publish/get/list API a client (or #23's CLI) calls.
type CapabilityService struct {
	udalv1.UnimplementedCapabilityServiceServer
	reg capability.Registry
}

// NewCapabilityService returns a CapabilityService backed by reg.
func NewCapabilityService(reg capability.Registry) *CapabilityService {
	return &CapabilityService{reg: reg}
}

func (s *CapabilityService) authorize(ctx context.Context, op auth.Operation) error {
	id, ok := auth.Authenticated(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no identity in context")
	}
	return auth.Authorize(id, op, "", nil)
}

func (s *CapabilityService) PublishSchema(ctx context.Context, req *udalv1.PublishSchemaRequest) (*udalv1.PublishSchemaResponse, error) {
	if err := s.authorize(ctx, auth.OpPublishSchema); err != nil {
		return nil, err
	}
	if len(req.GetSchema()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "schema is required")
	}
	published, err := s.reg.Publish(req.GetSchema())
	if err != nil {
		return nil, capabilityStatusError(err)
	}
	return &udalv1.PublishSchemaResponse{Schema: toProtoCapabilitySchema(published)}, nil
}

func (s *CapabilityService) GetSchema(ctx context.Context, req *udalv1.GetSchemaRequest) (*udalv1.GetSchemaResponse, error) {
	if err := s.authorize(ctx, auth.OpGetSchema); err != nil {
		return nil, err
	}
	if req.GetName() == "" || req.GetVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "name and version are required")
	}
	found, err := s.reg.Get(req.GetName(), req.GetVersion())
	if err != nil {
		return nil, capabilityStatusError(err)
	}
	return &udalv1.GetSchemaResponse{Schema: toProtoCapabilitySchema(found)}, nil
}

func (s *CapabilityService) ListSchemas(ctx context.Context, req *udalv1.ListSchemasRequest) (*udalv1.ListSchemasResponse, error) {
	if err := s.authorize(ctx, auth.OpListSchemas); err != nil {
		return nil, err
	}
	found, err := s.reg.List(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list schemas: %v", err)
	}
	pb := make([]*udalv1.CapabilitySchema, 0, len(found))
	for _, s := range found {
		pb = append(pb, toProtoCapabilitySchema(s))
	}
	return &udalv1.ListSchemasResponse{Schemas: pb}, nil
}

// capabilityStatusError maps a capability.Registry error to a gRPC status.
func capabilityStatusError(err error) error {
	switch {
	case errors.Is(err, capability.ErrInvalidSchema):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	case errors.Is(err, capability.ErrAlreadyExists):
		return status.Errorf(codes.AlreadyExists, "%v", err)
	case errors.Is(err, capability.ErrNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}

func toProtoCapabilitySchema(s capability.Schema) *udalv1.CapabilitySchema {
	pb := &udalv1.CapabilitySchema{
		Name:        s.Name,
		Version:     s.Version,
		Description: s.Description,
		Raw:         s.Raw,
	}
	if !s.PublishedAt.IsZero() {
		pb.PublishedAt = timestamppb.New(s.PublishedAt)
	}
	return pb
}
