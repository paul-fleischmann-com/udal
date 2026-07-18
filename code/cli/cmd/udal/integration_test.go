//go:build integration

package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
	"google.golang.org/grpc"
)

// TestSchemaPublishGetList_AgainstRealGateway is issue #23's flagged e2e
// coverage (see docs/features/plans/23-capability-registry-cli.md,
// "E2E-Testabdeckung"): schema_test.go's fakeCapabilityClient tests prove
// the CLI's own logic (ref parsing, sorting, formatting), but AC1
// specifically requires the CLI to surface "the same error the gateway API
// would return" — that's only checkable against the real
// CapabilityService/capability.Registry (#22), which lives under
// code/gateway/internal and can't be imported from this module (Go's
// internal/ visibility, different module tree). So this builds and runs
// the real gateway binary as a subprocess and drives the CLI's own
// cmdSchema* functions against it over a real gRPC connection.
func TestSchemaPublishGetList_AgainstRealGateway(t *testing.T) {
	gatewayBin := buildGatewayBinary(t)
	grpcAddr := freeAddr(t)
	restAddr := freeAddr(t)
	webhookAddr := freeAddr(t)
	const rawAPIKey = "cli-integration-test-key"

	cmd := exec.Command(gatewayBin)
	cmd.Env = append(os.Environ(),
		"UDAL_GRPC_ADDR="+grpcAddr,
		"UDAL_HTTP_ADDR="+restAddr,
		"UDAL_HTTP_WEBHOOK_ADDR="+webhookAddr,
		"UDAL_DEV_INSECURE=true",
		"UDAL_REGISTRY_PATH="+filepath.Join(t.TempDir(), "registry.db"),
		"UDAL_BOOTSTRAP_API_KEY=cli-itest:admin:"+rawAPIKey,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gateway: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("gateway stderr:\n%s", stderr.String())
		}
	})

	cf := &connectFlags{gateway: grpcAddr, insecure: true, apiKey: rawAPIKey}
	conn := waitForDial(t, cf)
	defer conn.Close()
	client := udalv1.NewCapabilityServiceClient(conn)
	ctx := cf.authContext(context.Background())

	// ── publish: invalid schema → CLI surfaces the server's exact error ──
	invalidPath := filepath.Join(t.TempDir(), "invalid.json")
	os.WriteFile(invalidPath, []byte(`{"udal":"1.0","kind":"DeviceCapability"}`), 0o600) // missing required metadata
	var stdout, cliStderr bytes.Buffer
	code := cmdSchemaPublish(ctx, client, &stdout, &cliStderr, invalidPath)
	if code != 1 {
		t.Fatalf("publish invalid schema: exit code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), cliStderr.String())
	}
	if !strings.Contains(cliStderr.String(), "invalid") {
		t.Errorf("expected the real server's validation error in stderr, got: %s", cliStderr.String())
	}

	// ── publish: valid schema → then get → roundtrip ──
	validPath := findExampleSchema(t, "temperature-sensor.json")
	stdout.Reset()
	cliStderr.Reset()
	code = cmdSchemaPublish(ctx, client, &stdout, &cliStderr, validPath)
	if code != 0 {
		t.Fatalf("publish valid schema: exit code = %d, want 0; stderr=%s", code, cliStderr.String())
	}
	published := strings.TrimSpace(strings.TrimPrefix(stdout.String(), "published "))
	if published == "" {
		t.Fatalf("could not parse published name@version from: %q", stdout.String())
	}

	stdout.Reset()
	cliStderr.Reset()
	code = cmdSchemaGet(ctx, client, &stdout, &cliStderr, published)
	if code != 0 {
		t.Fatalf("get: exit code = %d, want 0; stderr=%s", code, cliStderr.String())
	}
	original, _ := os.ReadFile(validPath)
	if !strings.Contains(stdout.String(), `"temperature-sensor"`) {
		t.Errorf("get output doesn't look like the published schema: %s", stdout.String())
	}
	_ = original

	// ── list: the just-published schema shows up ──
	stdout.Reset()
	cliStderr.Reset()
	code = cmdSchemaList(ctx, client, &stdout, &cliStderr, "temperature-sensor")
	if code != 0 {
		t.Fatalf("list: exit code = %d, want 0; stderr=%s", code, cliStderr.String())
	}
	if !strings.Contains(stdout.String(), "temperature-sensor") {
		t.Errorf("list output doesn't mention the published schema: %s", stdout.String())
	}

	// ── publish the same name@version again → AlreadyExists, verbatim ──
	stdout.Reset()
	cliStderr.Reset()
	code = cmdSchemaPublish(ctx, client, &stdout, &cliStderr, validPath)
	if code != 1 {
		t.Fatalf("republish: exit code = %d, want 1", code)
	}
	if !strings.Contains(cliStderr.String(), "already exists") {
		t.Errorf("expected an 'already exists' error from the real server, got: %s", cliStderr.String())
	}
}

func buildGatewayBinary(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	gatewayModDir := filepath.Join(wd, "..", "..", "..", "gateway")
	binPath := filepath.Join(t.TempDir(), "gateway")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/gateway")
	cmd.Dir = gatewayModDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build gateway binary: %v\n%s", err, out)
	}
	return binPath
}

func findExampleSchema(t *testing.T, name string) string {
	t.Helper()
	wd, _ := os.Getwd()
	path := filepath.Join(wd, "..", "..", "..", "..", "schema", "examples", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("example schema %s not found at %s: %v", name, path, err)
	}
	return path
}

func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	addr := lis.Addr().String()
	lis.Close()
	return addr
}

func waitForDial(t *testing.T, cf *connectFlags) *grpc.ClientConn {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := cf.dial()
		if err == nil {
			// grpc.NewClient doesn't actually connect until first RPC;
			// probe with a real (expected-to-fail) call so "waiting for
			// the gateway to start" and "gateway is up but this call is
			// unauthorized/whatever" are distinguishable — any response
			// (even an error response) means the server is listening.
			client := udalv1.NewCapabilityServiceClient(conn)
			_, callErr := client.ListSchemas(context.Background(), &udalv1.ListSchemasRequest{})
			if callErr == nil || !strings.Contains(callErr.Error(), "connection refused") {
				return conn
			}
			lastErr = callErr
			conn.Close()
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("gateway never became reachable: %v", lastErr)
	return nil
}
