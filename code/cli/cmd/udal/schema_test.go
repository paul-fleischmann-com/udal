package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeCapabilityClient implements udalv1.CapabilityServiceClient for unit
// tests that only need to exercise the CLI's own logic (ref parsing,
// sorting, JSON formatting, error passthrough) — the real-server case
// (issue #23's AC1: "the same error the gateway API would return") is
// covered separately by integration_test.go against a real gateway
// subprocess, since that's the class of bug a fake can't catch (see the
// plan doc's E2E-Testabdeckung).
type fakeCapabilityClient struct {
	publishResp *udalv1.PublishSchemaResponse
	publishErr  error
	getResp     *udalv1.GetSchemaResponse
	getErr      error
	listResp    *udalv1.ListSchemasResponse
	listErr     error
}

func (f *fakeCapabilityClient) PublishSchema(context.Context, *udalv1.PublishSchemaRequest, ...grpc.CallOption) (*udalv1.PublishSchemaResponse, error) {
	return f.publishResp, f.publishErr
}
func (f *fakeCapabilityClient) GetSchema(context.Context, *udalv1.GetSchemaRequest, ...grpc.CallOption) (*udalv1.GetSchemaResponse, error) {
	return f.getResp, f.getErr
}
func (f *fakeCapabilityClient) ListSchemas(context.Context, *udalv1.ListSchemasRequest, ...grpc.CallOption) (*udalv1.ListSchemasResponse, error) {
	return f.listResp, f.listErr
}

func TestCmdSchemaPublish_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(path, []byte(`{"udal":"1.0"}`), 0o600); err != nil {
		t.Fatalf("write test schema: %v", err)
	}
	fake := &fakeCapabilityClient{publishResp: &udalv1.PublishSchemaResponse{
		Schema: &udalv1.CapabilitySchema{Name: "temperature-sensor", Version: "1.0.0"},
	}}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaPublish(context.Background(), fake, &stdout, &stderr, path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "published temperature-sensor@1.0.0\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestCmdSchemaPublish_MissingFile(t *testing.T) {
	fake := &fakeCapabilityClient{}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaPublish(context.Background(), fake, &stdout, &stderr, "/does/not/exist.json")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on error, got %q", stdout.String())
	}
}

func TestCmdSchemaPublish_ServerErrorPassedThroughVerbatim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	os.WriteFile(path, []byte(`{}`), 0o600)

	serverErr := status.Error(codes.InvalidArgument, "capability: schema is invalid: metadata.name is required")
	fake := &fakeCapabilityClient{publishErr: serverErr}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaPublish(context.Background(), fake, &stdout, &stderr, path)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "capability: schema is invalid: metadata.name is required") {
		t.Errorf("stderr = %q, want it to contain the server's exact message", stderr.String())
	}
}

func TestCmdSchemaGet_PrettyPrintsJSON(t *testing.T) {
	fake := &fakeCapabilityClient{getResp: &udalv1.GetSchemaResponse{
		Schema: &udalv1.CapabilitySchema{Name: "x", Version: "1.0.0", Raw: []byte(`{"a":1,"b":2}`)},
	}}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaGet(context.Background(), fake, &stdout, &stderr, "x@1.0.0")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	var roundTrip map[string]int
	if err := json.Unmarshal(stdout.Bytes(), &roundTrip); err != nil {
		t.Fatalf("output isn't valid JSON: %v\noutput: %s", err, stdout.String())
	}
	if roundTrip["a"] != 1 || roundTrip["b"] != 2 {
		t.Errorf("roundTrip = %+v", roundTrip)
	}
	if !strings.Contains(stdout.String(), "\n  ") {
		t.Errorf("expected indented (pretty-printed) output, got: %s", stdout.String())
	}
}

func TestCmdSchemaGet_InvalidRef(t *testing.T) {
	fake := &fakeCapabilityClient{}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaGet(context.Background(), fake, &stdout, &stderr, "no-at-sign")
	if code != 2 {
		t.Errorf("exit code = %d, want 2 for a malformed ref", code)
	}
}

func TestCmdSchemaGet_NotFound(t *testing.T) {
	fake := &fakeCapabilityClient{getErr: status.Error(codes.NotFound, "capability: schema not found")}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaGet(context.Background(), fake, &stdout, &stderr, "x@9.9.9")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "schema not found") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestCmdSchemaList_SortsNewestFirst(t *testing.T) {
	older := timestamppb.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newer := timestamppb.New(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	newest := timestamppb.New(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	fake := &fakeCapabilityClient{listResp: &udalv1.ListSchemasResponse{
		// Deliberately out of order — the server (#22) makes no ordering
		// guarantee; the CLI must sort.
		Schemas: []*udalv1.CapabilitySchema{
			{Name: "x", Version: "1.0.0", PublishedAt: older},
			{Name: "x", Version: "1.2.0", PublishedAt: newest},
			{Name: "x", Version: "1.1.0", PublishedAt: newer},
		},
	}}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaList(context.Background(), fake, &stdout, &stderr, "x")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	i120 := strings.Index(out, "1.2.0")
	i110 := strings.Index(out, "1.1.0")
	i100 := strings.Index(out, "1.0.0")
	if !(i120 < i110 && i110 < i100) {
		t.Errorf("expected newest-first order 1.2.0, 1.1.0, 1.0.0, got:\n%s", out)
	}
}

func TestCmdSchemaList_Empty(t *testing.T) {
	fake := &fakeCapabilityClient{listResp: &udalv1.ListSchemasResponse{}}
	var stdout, stderr bytes.Buffer
	code := cmdSchemaList(context.Background(), fake, &stdout, &stderr, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "no schemas") {
		t.Errorf("stdout = %q, want a friendly empty message", stdout.String())
	}
}
