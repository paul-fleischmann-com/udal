package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	udalv1 "github.com/paulefl/udal/code/api/proto/gen/udal/v1"
)

func runSchemaPublishCmd(args []string) int {
	fs := flag.NewFlagSet("schema publish", flag.ContinueOnError)
	cf := &connectFlags{}
	cf.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: udal schema publish <file.json>")
		return 2
	}

	conn, err := cf.dial()
	if err != nil {
		fmt.Fprintf(os.Stderr, "udal: connect to gateway: %v\n", err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	client := udalv1.NewCapabilityServiceClient(conn)
	return cmdSchemaPublish(cf.authContext(context.Background()), client, os.Stdout, os.Stderr, fs.Arg(0))
}

// cmdSchemaPublish reads path and publishes it. Separate from
// runSchemaPublishCmd so tests can pass a client dialed against an
// in-process CapabilityService (see connect_test.go) and inspect the exact
// stdout/stderr output and exit code, without a subprocess or a real
// listening port.
func cmdSchemaPublish(ctx context.Context, client udalv1.CapabilityServiceClient, stdout, stderr io.Writer, path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "udal: read %s: %v\n", path, err)
		return 1
	}
	resp, err := client.PublishSchema(ctx, &udalv1.PublishSchemaRequest{Schema: raw})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "udal: %s\n", grpcMessage(err))
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "published %s@%s\n", resp.GetSchema().GetName(), resp.GetSchema().GetVersion())
	return 0
}
